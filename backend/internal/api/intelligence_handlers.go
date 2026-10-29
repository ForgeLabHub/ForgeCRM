package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/forecast"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/risk"
)

// This file is the HTTP surface of the Forecasting & Risk Lab. Every route
// declares a permission, every request body is size-limited and rejects unknown
// fields, and every error is mapped to the shared envelope.
//
// The DTOs here are the request/response contract the OpenAPI document is
// generated from. They are named in internal/api/contract.go; a route without a
// row there is left unspecified rather than guessed.

// mountIntelligence wires the Forecasting & Risk endpoints.
func (s *Server) mountIntelligence(r chi.Router) {
	r.Route("/forecast", func(r chi.Router) {
		// The catalog is static: it describes the methods the engine can compute and
		// exposes no tenant data, so it needs no permission beyond being signed in.
		r.Get("/catalog", s.handleForecastCatalog)
		r.With(s.perm(rbac.PermForecastRead)).Get("/runs", s.handleListForecastRuns)
		r.With(s.perm(rbac.PermForecastRun)).Post("/runs", s.handleCreateForecastRun)
		r.With(s.perm(rbac.PermForecastRead)).Get("/runs/{id}", s.handleGetForecastRun)
	})

	r.Route("/risk", func(r chi.Router) {
		r.With(s.perm(rbac.PermRiskRead)).Get("/signals", s.handleListRiskSignals)
		r.With(s.perm(rbac.PermRiskRead)).Post("/scan", s.handleRiskScan)
		r.With(s.perm(rbac.PermRiskRead)).Get("/signals/{id}", s.handleGetRiskSignal)
		r.With(s.perm(rbac.PermRiskRead)).Post("/signals/{id}/status", s.handleSetRiskSignalStatus)
		r.With(s.perm(rbac.PermRiskRead)).Get("/thresholds", s.handleGetRiskThresholds)
		r.With(s.perm(rbac.PermRiskManage)).Put("/thresholds", s.handleSetRiskThresholds)
	})
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

// forecastRunResponse is a stored forecast run. History and Result are exact JSON
// documents written by internal/analytics; Result carries money as integer minor
// units.
type forecastRunResponse struct {
	ID               uuid.UUID                `json:"id"`
	Method           analytics.ForecastMethod `json:"method"`
	AlgorithmVersion string                   `json:"algorithm_version"`
	Horizon          int                      `json:"horizon"`
	History          json.RawMessage          `json:"history"`
	Result           json.RawMessage          `json:"result"`
	CreatedAt        time.Time                `json:"created_at"`
}

// forecastRunListResponse wraps the run list.
type forecastRunListResponse struct {
	Runs []forecastRunResponse `json:"runs"`
}

// createForecastRunRequest is the request to compute and store a forecast.
type createForecastRunRequest struct {
	Method  analytics.ForecastMethod `json:"method"`
	Window  int                      `json:"window,omitempty"`
	Horizon int                      `json:"horizon"`
	Months  int                      `json:"months,omitempty"`
}

// forecastCatalogResponse wraps the forecasting catalog.
type forecastCatalogResponse struct {
	Methods []forecast.CatalogEntry `json:"methods"`
}

// riskSignalResponse is a persisted risk signal.
type riskSignalResponse struct {
	ID              uuid.UUID                `json:"id"`
	DetectedAt      time.Time                `json:"detected_at"`
	Type            analytics.RiskSignalType `json:"type"`
	Severity        analytics.RiskSeverity   `json:"severity"`
	Rule            string                   `json:"rule"`
	EntityType      string                   `json:"entity_type"`
	EntityID        string                   `json:"entity_id"`
	EntityLabel     string                   `json:"entity_label"`
	Rationale       string                   `json:"rationale"`
	Evidence        json.RawMessage          `json:"evidence"`
	Status          string                   `json:"status"`
	StatusChangedAt *time.Time               `json:"status_changed_at,omitempty"`
	ResolvedByScan  bool                     `json:"resolved_by_scan"`
}

// riskSignalListResponse wraps the signal list.
type riskSignalListResponse struct {
	Signals []riskSignalResponse `json:"signals"`
}

// riskScanResponse summarises a scan and returns the live list.
type riskScanResponse struct {
	Detected int                  `json:"detected"`
	Opened   int                  `json:"opened"`
	Updated  int                  `json:"updated"`
	Resolved int                  `json:"resolved"`
	Signals  []riskSignalResponse `json:"signals"`
}

// setRiskStatusRequest is the request to move a signal's status.
type setRiskStatusRequest struct {
	Status string `json:"status"`
}

// riskThresholdsResponse is the organization's thresholds.
type riskThresholdsResponse struct {
	StalledDealDays         int `json:"stalled_deal_days"`
	OverdueTaskDays         int `json:"overdue_task_days"`
	AccountNoContactDays    int `json:"account_no_contact_days"`
	AmountSpikeRatio        int `json:"amount_spike_ratio"`
	ConversionDropRatio     int `json:"conversion_drop_ratio"`
	StageConcentrationRatio int `json:"stage_concentration_ratio"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleForecastCatalog(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, forecastCatalogResponse{Methods: forecast.Catalog()})
}

func (s *Server) handleListForecastRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	runs, err := s.deps.Forecast.ListRuns(r.Context(), p)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	out := forecastRunListResponse{Runs: make([]forecastRunResponse, 0, len(runs))}
	for _, run := range runs {
		out.Runs = append(out.Runs, toForecastRunResponse(run))
	}
	httpx.WriteJSON(w, out)
}

func (s *Server) handleCreateForecastRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createForecastRunRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	run, err := s.deps.Forecast.RunForecast(r.Context(), p, forecast.RunInput{
		Method: req.Method, Window: req.Window, Horizon: req.Horizon, Months: req.Months,
	})
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, toForecastRunResponse(*run))
}

func (s *Server) handleGetForecastRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	run, err := s.deps.Forecast.GetRun(r.Context(), p, id)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toForecastRunResponse(*run))
}

func (s *Server) handleListRiskSignals(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := risk.ListFilter{
		Status:          q.Get("status"),
		Severity:        q.Get("severity"),
		Type:            q.Get("type"),
		IncludeTerminal: q.Get("include_terminal") == "true",
	}
	signals, err := s.deps.Risk.ListSignals(r.Context(), p, filter)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	out := riskSignalListResponse{Signals: make([]riskSignalResponse, 0, len(signals))}
	for _, sig := range signals {
		out.Signals = append(out.Signals, toRiskSignalResponse(sig))
	}
	httpx.WriteJSON(w, out)
}

func (s *Server) handleRiskScan(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	res, err := s.deps.Risk.Scan(r.Context(), p)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	out := riskScanResponse{
		Detected: res.Detected, Opened: res.Opened, Updated: res.Updated, Resolved: res.Resolved,
		Signals: make([]riskSignalResponse, 0, len(res.Signals)),
	}
	for _, sig := range res.Signals {
		out.Signals = append(out.Signals, toRiskSignalResponse(sig))
	}
	httpx.WriteJSON(w, out)
}

func (s *Server) handleGetRiskSignal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	sig, err := s.deps.Risk.GetSignal(r.Context(), p, id)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toRiskSignalResponse(*sig))
}

func (s *Server) handleSetRiskSignalStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req setRiskStatusRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sig, err := s.deps.Risk.SetStatus(r.Context(), p, id, req.Status)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toRiskSignalResponse(*sig))
}

func (s *Server) handleGetRiskThresholds(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	th, err := s.deps.Risk.Thresholds(r.Context(), p)
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toRiskThresholdsResponse(th))
}

func (s *Server) handleSetRiskThresholds(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req riskThresholdsResponse
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	th, err := s.deps.Risk.SetThresholds(r.Context(), p, risk.Thresholds{
		StalledDealDays:         req.StalledDealDays,
		OverdueTaskDays:         req.OverdueTaskDays,
		AccountNoContactDays:    req.AccountNoContactDays,
		AmountSpikeRatio:        req.AmountSpikeRatio,
		ConversionDropRatio:     req.ConversionDropRatio,
		StageConcentrationRatio: req.StageConcentrationRatio,
	})
	if err != nil {
		s.respondIntelligenceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toRiskThresholdsResponse(th))
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

func toForecastRunResponse(run forecast.Run) forecastRunResponse {
	return forecastRunResponse{
		ID:               run.ID,
		Method:           run.Method,
		AlgorithmVersion: run.AlgorithmVersion,
		Horizon:          run.Horizon,
		History:          run.History,
		Result:           run.Result,
		CreatedAt:        run.CreatedAt,
	}
}

func toRiskSignalResponse(sig risk.Signal) riskSignalResponse {
	return riskSignalResponse{
		ID:              sig.ID,
		DetectedAt:      sig.DetectedAt,
		Type:            sig.Type,
		Severity:        sig.Severity,
		Rule:            sig.Rule,
		EntityType:      sig.EntityType,
		EntityID:        sig.EntityID,
		EntityLabel:     sig.EntityLabel,
		Rationale:       sig.Rationale,
		Evidence:        sig.Evidence,
		Status:          sig.Status,
		StatusChangedAt: sig.StatusChangedAt,
		ResolvedByScan:  sig.ResolvedByScan,
	}
}

func toRiskThresholdsResponse(t risk.Thresholds) riskThresholdsResponse {
	return riskThresholdsResponse{
		StalledDealDays:         t.StalledDealDays,
		OverdueTaskDays:         t.OverdueTaskDays,
		AccountNoContactDays:    t.AccountNoContactDays,
		AmountSpikeRatio:        t.AmountSpikeRatio,
		ConversionDropRatio:     t.ConversionDropRatio,
		StageConcentrationRatio: t.StageConcentrationRatio,
	}
}

// respondIntelligenceError maps the Forecasting & Risk sentinel errors to the
// shared envelope.
func (s *Server) respondIntelligenceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, forecast.ErrNotFound), errors.Is(err, risk.ErrNotFound):
		httpx.WriteError(w, r, httpx.ErrNotFound)
	case errors.Is(err, forecast.ErrValidation), errors.Is(err, risk.ErrValidation),
		errors.Is(err, risk.ErrBadStatus):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidation, err.Error()))
	case errors.Is(err, analytics.ErrInvalidParameter),
		errors.Is(err, analytics.ErrForecastInsufficientData):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidation, err.Error()))
	case errors.Is(err, rbac.ErrForbidden):
		httpx.WriteError(w, r, httpx.ErrForbidden)
	default:
		s.respondDomainError(w, r, err)
	}
}
