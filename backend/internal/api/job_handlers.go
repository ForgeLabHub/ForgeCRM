package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountJobs registers background job routes.
func (s *Server) mountJobs(r chi.Router) {
	r.Route("/jobs", func(r chi.Router) {
		r.With(s.perm(rbac.PermJobsRead)).Get("/", s.handleListJobs)
		r.With(s.perm(rbac.PermJobsRead)).Get("/stats", s.handleJobStats)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermJobsRead)).Get("/", s.handleGetJob)
			r.With(s.perm(rbac.PermJobsManage)).Post("/cancel", s.handleCancelJob)
			r.With(s.perm(rbac.PermJobsManage)).Post("/retry", s.handleRetryJob)
		})
	})
}

// jobInOrganization loads a job and confirms it belongs to the caller's
// organization. A job from another tenant is reported as not found, so the
// endpoint cannot be used to probe for other tenants' job identifiers.
func (s *Server) jobInOrganization(w http.ResponseWriter, r *http.Request, id, orgID uuid.UUID) (*jobs.Job, bool) {
	job, err := s.deps.Jobs.Get(r.Context(), id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return nil, false
	}
	if job == nil || job.OrganizationID == nil || *job.OrganizationID != orgID {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return nil, false
	}
	return job, true
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	status := r.URL.Query().Get("status")
	kind := r.URL.Query().Get("kind")
	items, total, err := s.deps.Jobs.List(r.Context(), &p.OrganizationID, status, kind, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	job, ok := s.jobInOrganization(w, r, id, p.OrganizationID)
	if !ok {
		return
	}
	httpx.WriteJSON(w, job)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.jobInOrganization(w, r, id, p.OrganizationID); !ok {
		return
	}
	if err := s.deps.Jobs.Cancel(r.Context(), id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionJobCancelled), EntityType: "job", EntityID: id.String(),
	})
	httpx.WriteJSON(w, map[string]any{
		"status": "cancellation requested",
		"note":   "a running job stops at its next checkpoint",
	})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	job, ok := s.jobInOrganization(w, r, id, p.OrganizationID)
	if !ok {
		return
	}
	if job.Status != jobs.StatusFailed && job.Status != jobs.StatusCancelled {
		httpx.WriteError(w, r, httpx.NewError(409, httpx.CodeConflict,
			"only a failed or cancelled job can be retried"))
		return
	}
	// Retry resets the existing row rather than creating a second job, so the
	// attempt history stays with one record.
	if err := s.deps.Jobs.Retry(r.Context(), id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionJobRetried), EntityType: "job", EntityID: id.String(),
	})
	httpx.WriteJSONStatus(w, http.StatusAccepted, map[string]any{"job_id": id, "status": "queued"})
}

func (s *Server) handleJobStats(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	stats, err := s.deps.Jobs.Stats(r.Context(), &p.OrganizationID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"by_status": stats})
}
