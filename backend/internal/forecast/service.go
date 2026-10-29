// Package forecast implements the Forecasting half of the Forecasting & Risk
// Lab: computing a revenue forecast from the organization's observed history and
// storing it so it can be reviewed and re-run.
//
// The package is an application service in the same sense as simulate: it reads
// a series through crm.Service with the caller's principal, hands the series and
// the parameters to the pure arithmetic in internal/analytics, and persists the
// result. It never writes CRM data. The only writes are to the forecast_runs
// table the organization owns.
//
// A stored run keeps the series it was computed from, so a forecast stays
// explainable after the CRM data has moved on, and the algorithm version makes a
// result computed by an older method interpretable.
package forecast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Service is the Forecasting application service.
type Service struct {
	db    *database.DB
	crm   *crm.Service
	audit *audit.Logger
	now   func() time.Time
}

// NewService builds the forecasting service.
func NewService(db *database.DB, crmSvc *crm.Service, auditLog *audit.Logger) *Service {
	return &Service{db: db, crm: crmSvc, audit: auditLog, now: time.Now}
}

// Run is one persisted forecast computation.
type Run struct {
	ID               uuid.UUID                `json:"id"`
	OrganizationID   uuid.UUID                `json:"organization_id"`
	Method           analytics.ForecastMethod `json:"method"`
	AlgorithmVersion string                   `json:"algorithm_version"`
	Horizon          int                      `json:"horizon"`
	Params           json.RawMessage          `json:"params"`
	History          json.RawMessage          `json:"history"`
	Result           json.RawMessage          `json:"result"`
	CreatedBy        *uuid.UUID               `json:"created_by,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
}

// CatalogEntry describes one forecasting method for the catalog the UI shows.
type CatalogEntry struct {
	Method      analytics.ForecastMethod `json:"method"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	// Windowed is true when the method takes a moving-average window.
	Windowed bool `json:"windowed"`
	// DefaultWindow is the window the UI offers for a windowed method.
	DefaultWindow int `json:"default_window,omitempty"`
}

// Catalog returns the forecasting catalog. It is static data describing the
// methods the core can actually compute.
func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{
			Method: analytics.MethodMean, Name: "Historical average",
			Description: "Projects the average of the observed periods. The simplest method that uses all the data; a stable series makes it a strong baseline.",
		},
		{
			Method: analytics.MethodMovingAverage, Name: "Moving average",
			Description: "Projects the average of the last few periods. Responds to a recent level change that a whole-history average would smooth away.",
			Windowed:    true, DefaultWindow: 3,
		},
		{
			Method: analytics.MethodTrend, Name: "Trend line",
			Description: "Fits a straight line to the series and extends it. Only meaningful when the series is roughly linear; the measured accuracy says whether it was.",
		},
	}
}

// Sentinel errors mapped to HTTP statuses by the API layer.
var (
	// ErrNotFound is returned when a run does not exist in the caller's organization.
	ErrNotFound = errors.New("forecast run not found")
	// ErrValidation is returned for a request that is well-formed but invalid.
	ErrValidation = errors.New("invalid forecast request")
)

// RunInput is the request to compute and store a forecast.
type RunInput struct {
	Method  analytics.ForecastMethod
	Window  int
	Horizon int
	// Months is how many trailing months of history to read. Zero uses the default.
	Months int
}

// RunForecast reads the revenue history, computes the forecast, and stores it.
func (s *Service) RunForecast(ctx context.Context, p *rbac.Principal, in RunInput) (*Run, error) {
	if err := p.Require(rbac.PermForecastRun); err != nil {
		return nil, err
	}
	params := analytics.ForecastParams{Method: in.Method, Window: in.Window, Horizon: in.Horizon}

	history, err := s.crm.RevenueHistory(ctx, p, crm.RevenueHistoryInput{Months: in.Months})
	if err != nil {
		if errors.Is(err, rbac.ErrForbidden) {
			return nil, err
		}
		return nil, fmt.Errorf("read revenue history: %w", err)
	}
	points, err := toPeriodPoints(history)
	if err != nil {
		return nil, err
	}
	result, err := analytics.RunForecast(points, params)
	if err != nil {
		return nil, err
	}

	// The stored parameters are the full set, including the window, so a run can be
	// reproduced from its own row without the caller's original request.
	paramsJSON, err := json.Marshal(map[string]any{
		"method": params.Method, "window": params.Window, "horizon": params.Horizon,
	})
	if err != nil {
		return nil, fmt.Errorf("encode params: %w", err)
	}
	historyJSON, err := json.Marshal(history.Points)
	if err != nil {
		return nil, fmt.Errorf("encode history: %w", err)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}

	run := &Run{
		ID:               uuid.New(),
		OrganizationID:   p.OrganizationID,
		Method:           params.Method,
		AlgorithmVersion: analytics.ForecastAlgorithmVersion,
		Horizon:          params.Horizon,
		Params:           paramsJSON,
		History:          historyJSON,
		Result:           resultJSON,
		CreatedBy:        &p.UserID,
	}
	const q = `
		INSERT INTO forecast_runs
			(id, organization_id, created_by, method, algorithm_version, horizon, params, history, result)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING created_at`
	if err := s.db.Q(ctx).QueryRow(ctx, q, run.ID, run.OrganizationID, run.CreatedBy,
		run.Method, run.AlgorithmVersion, run.Horizon, run.Params, run.History, run.Result).
		Scan(&run.CreatedAt); err != nil {
		return nil, fmt.Errorf("save forecast run: %w", err)
	}

	if err := s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         "forecast.run.created",
		EntityType:     "forecast_run",
		EntityID:       run.ID.String(),
		Metadata:       map[string]any{"method": string(run.Method), "horizon": run.Horizon},
	}); err != nil {
		return nil, err
	}
	return run, nil
}

// ListRuns returns the organization's stored forecasts, newest first.
func (s *Service) ListRuns(ctx context.Context, p *rbac.Principal) ([]Run, error) {
	if err := p.Require(rbac.PermForecastRead); err != nil {
		return nil, err
	}
	const q = `
		SELECT id, organization_id, method, algorithm_version, horizon, params, history,
		       result, created_by, created_at
		FROM forecast_runs
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT 50`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list forecast runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.Method, &r.AlgorithmVersion,
			&r.Horizon, &r.Params, &r.History, &r.Result, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRun returns one stored forecast, scoped to the organization.
func (s *Service) GetRun(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Run, error) {
	if err := p.Require(rbac.PermForecastRead); err != nil {
		return nil, err
	}
	const q = `
		SELECT id, organization_id, method, algorithm_version, horizon, params, history,
		       result, created_by, created_at
		FROM forecast_runs
		WHERE id = $1 AND organization_id = $2`
	var r Run
	err := s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID).Scan(
		&r.ID, &r.OrganizationID, &r.Method, &r.AlgorithmVersion, &r.Horizon,
		&r.Params, &r.History, &r.Result, &r.CreatedBy, &r.CreatedAt)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load forecast run: %w", err)
	}
	return &r, nil
}

// toPeriodPoints converts the reader's decimal strings into exact analytics
// values. A parse failure is returned rather than defaulted, because a silent
// zero would make a forecast quietly wrong.
func toPeriodPoints(h *crm.RevenueHistory) ([]analytics.PeriodPoint, error) {
	out := make([]analytics.PeriodPoint, 0, len(h.Points))
	for _, pt := range h.Points {
		m, err := analytics.ParseMoney(pt.Amount, h.Currency)
		if err != nil {
			return nil, fmt.Errorf("parse revenue for %s (%q): %w", pt.Period, pt.Amount, err)
		}
		out = append(out, analytics.PeriodPoint{Period: pt.Period, Value: m})
	}
	return out, nil
}
