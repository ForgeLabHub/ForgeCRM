package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountIntegrations registers the Integration Hub routes.
func (s *Server) mountIntegrations(r chi.Router) {
	r.Route("/integrations", func(r chi.Router) {
		r.With(s.perm(rbac.PermIntegrationsRead)).Get("/", s.handleListIntegrations)
		r.With(s.perm(rbac.PermIntegrationsConfigure)).Post("/", s.handleCreateIntegration)
		r.With(s.perm(rbac.PermIntegrationsRead)).Get("/kinds", s.handleListIntegrationKinds)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermIntegrationsRead)).Get("/", s.handleGetIntegration)
			r.With(s.perm(rbac.PermIntegrationsConfigure)).Post("/test", s.handleTestIntegration)
			r.With(s.perm(rbac.PermIntegrationsConfigure)).Put("/status", s.handleSetIntegrationStatus)
			r.With(s.perm(rbac.PermIntegrationsConfigure)).Delete("/", s.handleRevokeIntegration)
			r.With(s.perm(rbac.PermIntegrationsRead)).Get("/runs", s.handleListIntegrationRuns)
			r.With(s.perm(rbac.PermIntegrationsRead)).Get("/status", s.handleIntegrationStatus)
		})
	})
}

func (s *Server) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Integrations.List(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"items": items})
}

// handleListIntegrationKinds documents which adapter kinds exist, so an operator
// does not have to guess a value that would be rejected.
func (s *Server) handleListIntegrationKinds(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, map[string]any{
		"items": []map[string]any{
			{"kind": "rest_api", "description": "Read records from a REST endpoint", "direction": "inbound"},
			{"kind": "webhook_out", "description": "Deliver signed webhooks to an external endpoint", "direction": "outbound"},
			{"kind": "smtp_probe", "description": "Check that an SMTP host and port are reachable", "direction": "outbound"},
			{"kind": "postgres", "description": "Database source, configured through the migrations API", "direction": "inbound"},
			{"kind": "sqlite", "description": "SQLite file source, configured through the migrations API", "direction": "inbound"},
		},
	})
}

func (s *Server) handleCreateIntegration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req integrations.CreateRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	integration, err := s.deps.Integrations.Create(r.Context(), p, req)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, integration)
}

func (s *Server) handleGetIntegration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	integration, err := s.deps.Integrations.Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, integration)
}

func (s *Server) handleTestIntegration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	status, err := s.deps.Integrations.TestConnection(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status)
}

type setIntegrationStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleSetIntegrationStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req setIntegrationStatusRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := s.deps.Integrations.SetStatus(r.Context(), p, id, req.Status); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "updated"})
}

func (s *Server) handleRevokeIntegration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.Integrations.Revoke(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"status": "revoked",
		"note":   "stored credentials have been deleted and the integration is archived",
	})
}

func (s *Server) handleListIntegrationRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	items, total, err := s.deps.Integrations.ListRuns(r.Context(), p, id, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleIntegrationStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	integration, err := s.deps.Integrations.Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"id":              integration.ID,
		"name":            integration.Name,
		"kind":            integration.Kind,
		"status":          integration.Status,
		"allow_write":     integration.AllowWrite,
		"has_secrets":     integration.HasSecrets,
		"last_test_at":    integration.LastTestAt,
		"last_test_ok":    integration.LastTestOK,
		"last_test_error": integration.LastTestError,
		"last_sync_at":    integration.LastSyncAt,
		"timeout_seconds": integration.TimeoutSeconds,
		"max_concurrency": integration.MaxConcurrency,
	})
}
