package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/simulate"
)

// This file is the HTTP surface of the Simulation Lab. Every route declares a
// permission, every request body is size-limited and rejects unknown fields, and
// every error is mapped to the shared envelope.
//
// The DTOs here are the request/response contract the OpenAPI document is
// generated from. They are named in internal/api/contract.go; a route without a
// row there is left unspecified rather than guessed.

// mountSimulation wires the Simulation Lab endpoints.
func (s *Server) mountSimulation(r chi.Router) {
	r.Route("/simulation", func(r chi.Router) {
		// The catalog describes the scenarios the engine can compute. It is static
		// data, so it needs no permission beyond being signed in; it exposes no
		// tenant data.
		r.Get("/catalog", s.handleSimulationCatalog)

		r.Route("/scenarios", func(r chi.Router) {
			r.With(s.perm(rbac.PermSimulationRead)).Get("/", s.handleListScenarios)
			r.With(s.perm(rbac.PermSimulationManage)).Post("/", s.handleCreateScenario)
			r.Route("/{id}", func(r chi.Router) {
				r.With(s.perm(rbac.PermSimulationRead)).Get("/", s.handleGetScenario)
				r.With(s.perm(rbac.PermSimulationManage)).Delete("/", s.handleArchiveScenario)
				r.With(s.perm(rbac.PermSimulationRun)).Post("/runs", s.handleRunScenario)
				r.With(s.perm(rbac.PermSimulationRead)).Get("/runs", s.handleListScenarioRuns)
			})
		})

		// A preview computes an unsaved parameter set. It persists nothing, so it
		// needs run but not manage.
		r.With(s.perm(rbac.PermSimulationRun)).Post("/preview", s.handleSimulationPreview)
	})
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

// scenarioResponse is a saved scenario as returned to the client. Params is
// passed through as the stored document, so the client reads exactly what was
// saved.
type scenarioResponse struct {
	ID               uuid.UUID       `json:"id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	Kind             analytics.Kind  `json:"kind"`
	Params           json.RawMessage `json:"params"`
	AlgorithmVersion string          `json:"algorithm_version"`
	OwnerUserID      *uuid.UUID      `json:"owner_user_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// scenarioListResponse wraps the scenario list.
type scenarioListResponse struct {
	Scenarios []scenarioResponse `json:"scenarios"`
}

// createScenarioRequest is the request to save a scenario.
type createScenarioRequest struct {
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Kind        analytics.Kind  `json:"kind"`
	Params      json.RawMessage `json:"params"`
}

// runResponse is a persisted run. Baseline and Result are exact JSON documents
// written by internal/analytics; Result carries money as integer minor units.
type runResponse struct {
	ID               uuid.UUID       `json:"id"`
	ScenarioID       *uuid.UUID      `json:"scenario_id,omitempty"`
	Kind             analytics.Kind  `json:"kind"`
	AlgorithmVersion string          `json:"algorithm_version"`
	Params           json.RawMessage `json:"params"`
	Baseline         json.RawMessage `json:"baseline"`
	Result           json.RawMessage `json:"result"`
	CreatedAt        time.Time       `json:"created_at"`
}

// runListResponse wraps the run list.
type runListResponse struct {
	Runs []runResponse `json:"runs"`
}

// simulationRunRequest selects what to read for a run.
type simulationRunRequest struct {
	PipelineID *uuid.UUID `json:"pipeline_id,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
}

// previewRequest is an unsaved scenario to compute.
type previewRequest struct {
	Kind       analytics.Kind  `json:"kind"`
	Params     json.RawMessage `json:"params"`
	PipelineID *uuid.UUID      `json:"pipeline_id,omitempty"`
	From       *time.Time      `json:"from,omitempty"`
	To         *time.Time      `json:"to,omitempty"`
}

// previewResponse is the preview result: the comparison, the baseline it was
// computed against, and any warnings the baseline reader raised.
type previewResponse struct {
	Comparison *analytics.Comparison `json:"comparison"`
	Baseline   *analytics.Baseline   `json:"baseline"`
	Warnings   []string              `json:"warnings,omitempty"`
}

// catalogResponse wraps the scenario catalog.
type catalogResponse struct {
	Scenarios []simulate.CatalogEntry `json:"scenarios"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleSimulationCatalog(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, catalogResponse{Scenarios: simulate.Catalog()})
}

func (s *Server) handleListScenarios(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	list, err := s.deps.Simulation.ListScenarios(r.Context(), p)
	if err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	out := scenarioListResponse{Scenarios: make([]scenarioResponse, 0, len(list))}
	for _, sc := range list {
		out.Scenarios = append(out.Scenarios, toScenarioResponse(sc))
	}
	httpx.WriteJSON(w, out)
}

func (s *Server) handleCreateScenario(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createScenarioRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sc, err := s.deps.Simulation.CreateScenario(r.Context(), p, simulate.CreateScenarioInput{
		Name:        req.Name,
		Description: req.Description,
		Kind:        req.Kind,
		Params:      req.Params,
	})
	if err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, toScenarioResponse(*sc))
}

func (s *Server) handleGetScenario(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	sc, err := s.deps.Simulation.GetScenario(r.Context(), p, id)
	if err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	httpx.WriteJSON(w, toScenarioResponse(*sc))
}

func (s *Server) handleArchiveScenario(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.Simulation.ArchiveScenario(r.Context(), p, id); err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunScenario(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req simulationRunRequest
	// The body is optional; an empty body means "use the defaults". A read error
	// other than EOF is a real error.
	if r.ContentLength > 0 {
		if err := decodeBody(w, r, &req); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	run, err := s.deps.Simulation.RunScenario(r.Context(), p, id, simulate.RunInput{
		PipelineID: req.PipelineID, From: req.From, To: req.To,
	})
	if err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, toRunResponse(*run))
}

func (s *Server) handleListScenarioRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	runs, err := s.deps.Simulation.ListRuns(r.Context(), p, id)
	if err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	out := runListResponse{Runs: make([]runResponse, 0, len(runs))}
	for _, run := range runs {
		out.Runs = append(out.Runs, toRunResponse(run))
	}
	httpx.WriteJSON(w, out)
}

func (s *Server) handleSimulationPreview(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req previewRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := s.deps.Simulation.Preview(r.Context(), p, simulate.PreviewInput{
		Kind: req.Kind, Params: req.Params,
		PipelineID: req.PipelineID, From: req.From, To: req.To,
	})
	if err != nil {
		s.respondSimulationError(w, r, err)
		return
	}
	httpx.WriteJSON(w, previewResponse{
		Comparison: res.Comparison,
		Baseline:   res.Baseline,
		Warnings:   res.Warnings,
	})
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

func toScenarioResponse(sc simulate.Scenario) scenarioResponse {
	return scenarioResponse{
		ID:               sc.ID,
		Name:             sc.Name,
		Description:      sc.Description,
		Kind:             sc.Kind,
		Params:           sc.Params,
		AlgorithmVersion: sc.AlgorithmVersion,
		OwnerUserID:      sc.OwnerUserID,
		CreatedAt:        sc.CreatedAt,
		UpdatedAt:        sc.UpdatedAt,
	}
}

func toRunResponse(run simulate.Run) runResponse {
	return runResponse{
		ID:               run.ID,
		ScenarioID:       run.ScenarioID,
		Kind:             run.Kind,
		AlgorithmVersion: run.AlgorithmVersion,
		Params:           run.Params,
		Baseline:         run.Baseline,
		Result:           run.Result,
		CreatedAt:        run.CreatedAt,
	}
}

// respondSimulationError maps the Simulation Lab's sentinel errors to the shared
// envelope. Insufficient data is a 422 with a specific message: the request was
// valid, the CRM simply does not hold enough history, and the user should be told
// which it is rather than receiving a generic validation failure.
func (s *Server) respondSimulationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, simulate.ErrNotFound):
		httpx.WriteError(w, r, httpx.ErrNotFound)
	case errors.Is(err, simulate.ErrNameTaken):
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict, err.Error()))
	case errors.Is(err, simulate.ErrValidation):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidation, err.Error()))
	case errors.Is(err, analytics.ErrInsufficientData):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidation, err.Error()))
	case errors.Is(err, analytics.ErrInvalidParameter):
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidation, err.Error()))
	case errors.Is(err, rbac.ErrForbidden):
		httpx.WriteError(w, r, httpx.ErrForbidden)
	default:
		s.respondDomainError(w, r, err)
	}
}
