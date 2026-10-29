// Package simulate implements the Simulation Lab: saved "what if" scenarios and
// the runs that compute them.
//
// The package is an application service in the same sense as importx or
// migratex. It orchestrates the CRM core to read a baseline, hands the baseline
// and the user's parameters to the pure arithmetic in internal/analytics, and
// persists the result. It never writes CRM data: the only reads go through
// crm.Service with the caller's principal, and the only writes are to the two
// simulation tables the user owns.
//
// A run is stored with the parameters and the baseline it was computed from, so
// a result stays reproducible and explainable after the underlying CRM data has
// moved on. See migration 0019.
package simulate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Service is the Simulation Lab application service.
type Service struct {
	db    *database.DB
	crm   *crm.Service
	audit *audit.Logger
	// now is overridable in tests.
	now func() time.Time
}

// NewService builds the Simulation Lab service.
func NewService(db *database.DB, crmSvc *crm.Service, auditLog *audit.Logger) *Service {
	return &Service{db: db, crm: crmSvc, audit: auditLog, now: time.Now}
}

// Scenario is a saved "what if" definition.
type Scenario struct {
	ID               uuid.UUID       `json:"id"`
	OrganizationID   uuid.UUID       `json:"organization_id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	Kind             analytics.Kind  `json:"kind"`
	Params           json.RawMessage `json:"params"`
	AlgorithmVersion string          `json:"algorithm_version"`
	CreatedBy        *uuid.UUID      `json:"created_by,omitempty"`
	OwnerUserID      *uuid.UUID      `json:"owner_user_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	ArchivedAt       *time.Time      `json:"archived_at,omitempty"`
}

// Run is one persisted computation.
type Run struct {
	ID               uuid.UUID       `json:"id"`
	OrganizationID   uuid.UUID       `json:"organization_id"`
	ScenarioID       *uuid.UUID      `json:"scenario_id,omitempty"`
	Kind             analytics.Kind  `json:"kind"`
	AlgorithmVersion string          `json:"algorithm_version"`
	Params           json.RawMessage `json:"params"`
	Baseline         json.RawMessage `json:"baseline"`
	Result           json.RawMessage `json:"result"`
	CreatedBy        *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

// CatalogEntry describes one scenario kind for the catalog the UI shows.
type CatalogEntry struct {
	Kind        analytics.Kind `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  []ParamSpec    `json:"parameters"`
}

// ParamSpec describes one tunable parameter, so the UI can render a labelled
// control with a default and a range rather than guessing.
type ParamSpec struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Default     string `json:"default"`
	// Kind is "factor" or "percent"; the UI labels the control accordingly.
	Kind string `json:"kind"`
	// StageScoped is true when the parameter is per pipeline stage (the funnel
	// conversion), which the UI renders as a row per stage.
	StageScoped bool `json:"stage_scoped"`
}

// Catalog returns the scenario catalog. It is static data: the scenarios the
// engine can actually compute, described in the terms the engine uses.
func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{
			Kind:        analytics.KindFunnel,
			Name:        "Sales funnel",
			Description: "Change stage conversions or deal inflow and see the expected number of closed deals and their value.",
			Parameters: []ParamSpec{
				{Key: "lead_volume", Label: "Deal inflow", Kind: "percent", Default: "0",
					Description: "Change the number of deals entering the funnel, e.g. 20 for a 20% increase."},
				{Key: "stage_conversion", Label: "Stage conversion", Kind: "percent", Default: "0",
					StageScoped: true,
					Description: "Change a stage's win probability, e.g. 10 for a 10% relative increase."},
			},
		},
		{
			Kind:        analytics.KindRevenue,
			Name:        "Revenue and terms",
			Description: "Change average ticket, win rate or deal volume and compare expected revenue with the baseline.",
			Parameters: []ParamSpec{
				{Key: "avg_ticket", Label: "Average ticket", Kind: "percent", Default: "0",
					Description: "Change the average deal value, e.g. -5 for a 5% reduction."},
				{Key: "win_rate", Label: "Win rate", Kind: "percent", Default: "0",
					Description: "Change the win rate, e.g. 10 for a 10% relative improvement."},
				{Key: "deal_count", Label: "Deal count", Kind: "percent", Default: "0",
					Description: "Change the number of deals in play, e.g. 25 for a 25% increase."},
			},
		},
		{
			Kind:        analytics.KindRetention,
			Name:        "Customer retention",
			Description: "Change the observed churn rate and see the customers retained and the financial effect. A parametric model, not a trained prediction.",
			Parameters: []ParamSpec{
				{Key: "churn_multiplier", Label: "Churn change", Kind: "percent", Default: "0",
					Description: "Change the churn rate, e.g. -20 to reduce churn by 20%."},
			},
		},
	}
}

// Sentinel errors mapped to HTTP statuses by the API layer.
var (
	// ErrNotFound is returned when a scenario does not exist in the caller's
	// organization.
	ErrNotFound = errors.New("simulation scenario not found")
	// ErrNameTaken is returned when a live scenario already has the name.
	ErrNameTaken = errors.New("a scenario with that name already exists")
	// ErrValidation is returned for a request that is well-formed but invalid.
	ErrValidation = errors.New("invalid scenario")
)

// CreateScenarioInput is the request to save a scenario.
type CreateScenarioInput struct {
	Name        string
	Description *string
	Kind        analytics.Kind
	Params      json.RawMessage
}

// CreateScenario saves a scenario after validating its kind and parameters.
func (s *Service) CreateScenario(ctx context.Context, p *rbac.Principal, in CreateScenarioInput) (*Scenario, error) {
	if err := p.Require(rbac.PermSimulationManage); err != nil {
		return nil, err
	}
	if !in.Kind.Valid() {
		return nil, fmt.Errorf("%w: unknown scenario kind %q", ErrValidation, in.Kind)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: a name is required", ErrValidation)
	}
	// Validate the parameters now, so a saved scenario cannot hold parameters that
	// will fail at run time. The parse is the same one a run performs.
	if _, err := parseParams(in.Kind, in.Params); err != nil {
		return nil, err
	}
	if len(in.Params) == 0 {
		in.Params = json.RawMessage(`{}`)
	}

	sc := &Scenario{
		ID:               uuid.New(),
		OrganizationID:   p.OrganizationID,
		Name:             name,
		Description:      in.Description,
		Kind:             in.Kind,
		Params:           in.Params,
		AlgorithmVersion: analytics.AlgorithmVersion,
		CreatedBy:        &p.UserID,
		OwnerUserID:      &p.UserID,
	}
	const q = `
		INSERT INTO simulation_scenarios
			(id, organization_id, created_by, owner_user_id, name, description, kind,
			 params, algorithm_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING created_at, updated_at`
	err := s.db.Q(ctx).QueryRow(ctx, q, sc.ID, sc.OrganizationID, sc.CreatedBy, sc.OwnerUserID,
		sc.Name, sc.Description, sc.Kind, sc.Params, sc.AlgorithmVersion).
		Scan(&sc.CreatedAt, &sc.UpdatedAt)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: %q", ErrNameTaken, name)
		}
		return nil, fmt.Errorf("save scenario: %w", err)
	}

	if err := s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         "simulation.scenario.created",
		EntityType:     "simulation_scenario",
		EntityID:       sc.ID.String(),
		After:          map[string]any{"name": sc.Name, "kind": sc.Kind},
	}); err != nil {
		return nil, err
	}
	return sc, nil
}

// ListScenarios returns the organization's live scenarios, newest first.
func (s *Service) ListScenarios(ctx context.Context, p *rbac.Principal) ([]Scenario, error) {
	if err := p.Require(rbac.PermSimulationRead); err != nil {
		return nil, err
	}
	const q = `
		SELECT id, organization_id, name, description, kind, params, algorithm_version,
		       created_by, owner_user_id, created_at, updated_at, archived_at
		FROM simulation_scenarios
		WHERE organization_id = $1 AND archived_at IS NULL
		ORDER BY created_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list scenarios: %w", err)
	}
	defer rows.Close()
	var out []Scenario
	for rows.Next() {
		var sc Scenario
		if err := rows.Scan(&sc.ID, &sc.OrganizationID, &sc.Name, &sc.Description, &sc.Kind,
			&sc.Params, &sc.AlgorithmVersion, &sc.CreatedBy, &sc.OwnerUserID,
			&sc.CreatedAt, &sc.UpdatedAt, &sc.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// GetScenario returns one scenario, scoped to the organization.
func (s *Service) GetScenario(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Scenario, error) {
	if err := p.Require(rbac.PermSimulationRead); err != nil {
		return nil, err
	}
	return s.getScenario(ctx, p.OrganizationID, id)
}

func (s *Service) getScenario(ctx context.Context, orgID, id uuid.UUID) (*Scenario, error) {
	const q = `
		SELECT id, organization_id, name, description, kind, params, algorithm_version,
		       created_by, owner_user_id, created_at, updated_at, archived_at
		FROM simulation_scenarios
		WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL`
	var sc Scenario
	err := s.db.Q(ctx).QueryRow(ctx, q, id, orgID).Scan(
		&sc.ID, &sc.OrganizationID, &sc.Name, &sc.Description, &sc.Kind,
		&sc.Params, &sc.AlgorithmVersion, &sc.CreatedBy, &sc.OwnerUserID,
		&sc.CreatedAt, &sc.UpdatedAt, &sc.ArchivedAt)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load scenario: %w", err)
	}
	return &sc, nil
}

// ArchiveScenario soft-deletes a scenario. Its runs are kept: a result someone
// already acted on should not vanish, and the run record is immutable anyway.
func (s *Service) ArchiveScenario(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	if err := p.Require(rbac.PermSimulationManage); err != nil {
		return err
	}
	const q = `
		UPDATE simulation_scenarios SET archived_at = now()
		WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL`
	tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
	if err != nil {
		return fmt.Errorf("archive scenario: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         "simulation.scenario.archived",
		EntityType:     "simulation_scenario",
		EntityID:       id.String(),
	})
}
