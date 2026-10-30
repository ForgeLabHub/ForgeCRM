package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/views"
)

// mountViews registers the saved list-view routes.
//
// Reading views needs views.read and changing them views.manage. The service
// enforces the finer rule the permission cannot express — a shared view is
// readable by the organization but only editable by its owner — so these gates
// control who may keep views at all, not who may edit someone else's.
func (s *Server) mountViews(r chi.Router) {
	r.Route("/views", func(r chi.Router) {
		r.Use(s.requireService(func() bool { return s.deps.Views != nil }, "saved views"))
		r.With(s.perm(rbac.PermViewsRead)).Get("/", s.handleListViews)
		r.With(s.perm(rbac.PermViewsManage)).Post("/", s.handleCreateView)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermViewsRead)).Get("/", s.handleGetView)
			r.With(s.perm(rbac.PermViewsManage)).Put("/", s.handleUpdateView)
			r.With(s.perm(rbac.PermViewsManage)).Delete("/", s.handleDeleteView)
		})
	})
}

func (s *Server) handleListViews(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	entityType := views.EntityType(r.URL.Query().Get("entity_type"))
	if entityType == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"entity_type": "is required",
		}))
		return
	}
	items, err := s.deps.Views.List(r.Context(), p, entityType)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

func (s *Server) handleGetView(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	v, err := s.deps.Views.Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, v)
}

func (s *Server) handleCreateView(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in views.Input
	if err := httpx.DecodeJSON(w, r, &in, 16*1024); err != nil {
		return
	}
	v, err := s.deps.Views.Create(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateView(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in views.Input
	if err := httpx.DecodeJSON(w, r, &in, 16*1024); err != nil {
		return
	}
	v, err := s.deps.Views.Update(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, v)
}

func (s *Server) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.Views.Delete(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
