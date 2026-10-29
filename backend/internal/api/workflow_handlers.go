package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// mountWorkflows registers the workflow automation routes.
//
// Read is gated by workflows.read; authoring by workflows.manage; running by
// workflows.run. The engine re-checks the author's permissions on every action, so
// these gates control who may define and start automation, not what the automation
// is ultimately allowed to touch.
func (s *Server) mountWorkflows(r chi.Router) {
	r.Route("/workflows", func(r chi.Router) {
		r.With(s.perm(rbac.PermWorkflowsRead)).Get("/", s.handleListWorkflows)
		r.With(s.perm(rbac.PermWorkflowsManage)).Post("/", s.handleCreateWorkflow)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermWorkflowsRead)).Get("/", s.handleGetWorkflow)
			r.With(s.perm(rbac.PermWorkflowsManage)).Put("/", s.handleUpdateWorkflow)
			r.With(s.perm(rbac.PermWorkflowsManage)).Put("/enabled", s.handleSetWorkflowEnabled)
			r.With(s.perm(rbac.PermWorkflowsManage)).Delete("/", s.handleArchiveWorkflow)
			r.With(s.perm(rbac.PermWorkflowsRun)).Post("/run", s.handleRunWorkflow)
			r.With(s.perm(rbac.PermWorkflowsRead)).Get("/runs", s.handleListWorkflowRuns)
		})
	})

	r.Route("/workflow-runs", func(r chi.Router) {
		r.With(s.perm(rbac.PermWorkflowsRead)).Get("/{id}", s.handleGetWorkflowRun)
		r.With(s.perm(rbac.PermWorkflowsRead)).Get("/{id}/steps", s.handleListWorkflowRunSteps)
	})
}

func (s *Server) workflows() *workflow.Engine { return s.deps.Workflows }

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	items, total, err := s.workflows().Store().List(r.Context(), p, includeArchived, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req workflow.CreateInput
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	wf, err := s.workflows().Store().Create(r.Context(), p, req)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	s.auditWorkflow(r, p, audit.ActionWorkflowCreated, wf.ID, map[string]any{"name": wf.Name})
	httpx.WriteJSONStatus(w, http.StatusCreated, wf)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid workflow id"))
		return
	}
	wf, err := s.workflows().Store().Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, wf)
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid workflow id"))
		return
	}
	var req workflow.CreateInput
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	wf, err := s.workflows().Store().Update(r.Context(), p, id, req)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	s.auditWorkflow(r, p, audit.ActionWorkflowUpdated, wf.ID, map[string]any{"name": wf.Name})
	httpx.WriteJSON(w, wf)
}

func (s *Server) handleSetWorkflowEnabled(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid workflow id"))
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	wf, err := s.workflows().Store().SetEnabled(r.Context(), p, id, req.Enabled)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	s.auditWorkflow(r, p, audit.ActionWorkflowEnabled, wf.ID, map[string]any{"enabled": req.Enabled})
	httpx.WriteJSON(w, wf)
}

func (s *Server) handleArchiveWorkflow(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid workflow id"))
		return
	}
	if err := s.workflows().Store().Archive(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	s.auditWorkflow(r, p, audit.ActionWorkflowArchived, id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleRunWorkflow runs a workflow on demand. It is synchronous: a manual run is
// expected to be a small, immediate thing, and the operator is waiting to see the
// result. A run that pauses on a delay returns 202 with the run id so the caller
// can follow it.
func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid workflow id"))
		return
	}
	wf, err := s.workflows().Store().Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	if !wf.Enabled {
		httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict, "the workflow is disabled"))
		return
	}
	result, err := s.workflows().RunManual(r.Context(), p, *wf)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Status == "paused" {
		status = http.StatusAccepted
	}
	httpx.WriteJSONStatus(w, status, result)
}

func (s *Server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid workflow id"))
		return
	}
	pg := httpx.ParsePagination(r)
	items, total, err := s.workflows().Store().ListRuns(r.Context(), p, id, r.URL.Query().Get("status"), pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid run id"))
		return
	}
	run, err := s.workflows().Store().GetRun(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, run)
}

func (s *Server) handleListWorkflowRunSteps(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation, "invalid run id"))
		return
	}
	steps, err := s.workflows().Store().ListRunSteps(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, steps)
}

func (s *Server) auditWorkflow(r *http.Request, p *rbac.Principal, action audit.Action, id uuid.UUID, meta map[string]any) {
	orgID := p.OrganizationID
	userID := p.UserID
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &orgID,
		ActorUserID:    &userID,
		ActorType:      "user",
		Action:         string(action),
		EntityType:     "workflow",
		EntityID:       id.String(),
		Metadata:       meta,
	})
}
