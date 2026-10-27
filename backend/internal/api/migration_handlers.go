package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountMigrations registers the data migration routes.
func (s *Server) mountMigrations(r chi.Router) {
	r.Route("/migrations", func(r chi.Router) {
		r.With(s.perm(rbac.PermMigrationsRead)).Get("/", s.handleListMigrations)
		r.With(s.perm(rbac.PermMigrationsManage)).Post("/", s.handleCreateMigration)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermMigrationsRead)).Get("/", s.handleGetMigration)
			r.With(s.perm(rbac.PermMigrationsRead)).Post("/discover", s.handleDiscoverMigrationSchema)
			r.With(s.perm(rbac.PermMigrationsRead)).Post("/reconciliation", s.handleReconcileMigration)
			r.With(s.perm(rbac.PermMigrationsRead)).Get("/reconciliation", s.handleListReconciliations)
		})
	})
}

func (s *Server) handleListMigrations(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Migrations.List(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"items": items})
}

// createMigrationRequest carries the connection configuration. The password is a
// separate field so it is never part of the stored configuration blob.
type createMigrationRequest struct {
	Name         string                    `json:"name"`
	Config       migratex.ConnectionConfig `json:"config"`
	Strategy     string                    `json:"strategy"`
	Password     string                    `json:"password"`
	SourceTables []string                  `json:"source_tables"`
}

func (s *Server) handleCreateMigration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createMigrationRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	job, err := s.deps.Migrations.Create(r.Context(), p, migratex.CreateRequest{
		Name:         req.Name,
		Config:       req.Config,
		Strategy:     req.Strategy,
		Password:     req.Password,
		SourceTables: req.SourceTables,
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, job)
}

func (s *Server) handleGetMigration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	job, err := s.deps.Migrations.Get(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	// The response never includes the stored credentials.
	httpx.WriteJSON(w, job)
}

func (s *Server) handleDiscoverMigrationSchema(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	tables, err := s.deps.Migrations.DiscoverSchema(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"tables": tables, "read_only": true})
}

type reconcileRequest struct {
	SourceTable  string `json:"source_table"`
	TargetEntity string `json:"target_entity"`
}

func (s *Server) handleReconcileMigration(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req reconcileRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rec, err := s.deps.Migrations.Reconcile(r.Context(), p, id, req.SourceTable, req.TargetEntity)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, rec)
}

func (s *Server) handleListReconciliations(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	items, err := s.deps.Migrations.ListReconciliations(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"items": items})
}
