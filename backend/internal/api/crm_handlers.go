package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountSystem registers system routes.
func (s *Server) mountSystemRoutes(r chi.Router) {
	r.Get("/system/info", s.handleSystemInfo)
	r.Get("/system/capabilities", s.handleCapabilities)
	r.With(s.perm(rbac.PermMetricsRead)).Get("/admin/metrics", s.handleMetrics)
}

// mountCRM registers the CRM routes.
func (s *Server) mountCRM(r chi.Router) {
	// Accounts
	r.Route("/accounts", func(r chi.Router) {
		r.With(s.perm(rbac.PermAccountsRead)).Get("/", s.handleListAccounts)
		r.With(s.perm(rbac.PermAccountsCreate)).Post("/", s.handleCreateAccount)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermAccountsRead)).Get("/", s.handleGetAccount)
			r.With(s.perm(rbac.PermAccountsUpdate)).Put("/", s.handleUpdateAccount)
			r.With(s.perm(rbac.PermAccountsArchive)).Delete("/", s.handleArchiveAccount)
			r.With(s.perm(rbac.PermContactsRead)).Get("/contacts", s.handleAccountContacts)
			r.With(s.perm(rbac.PermDealsRead)).Get("/deals", s.handleAccountDeals)
			r.With(s.perm(rbac.PermActivitiesRead)).Get("/activities", s.handleAccountActivities)
		})
	})

	// Contacts
	r.Route("/contacts", func(r chi.Router) {
		r.With(s.perm(rbac.PermContactsRead)).Get("/", s.handleListContacts)
		r.With(s.perm(rbac.PermContactsCreate)).Post("/", s.handleCreateContact)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermContactsRead)).Get("/", s.handleGetContact)
			r.With(s.perm(rbac.PermContactsUpdate)).Put("/", s.handleUpdateContact)
			r.With(s.perm(rbac.PermContactsArchive)).Delete("/", s.handleArchiveContact)
		})
	})

	// Leads
	r.Route("/leads", func(r chi.Router) {
		r.With(s.perm(rbac.PermLeadsRead)).Get("/", s.handleListLeads)
		r.With(s.perm(rbac.PermLeadsCreate)).Post("/", s.handleCreateLead)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermLeadsRead)).Get("/", s.handleGetLead)
			r.With(s.perm(rbac.PermLeadsUpdate)).Put("/", s.handleUpdateLead)
			r.With(s.perm(rbac.PermLeadsConvert)).Post("/convert", s.handleConvertLead)
		})
	})

	// Pipelines
	r.Route("/pipelines", func(r chi.Router) {
		r.With(s.perm(rbac.PermPipelinesRead)).Get("/", s.handleListPipelines)
		r.With(s.perm(rbac.PermPipelinesManage)).Post("/", s.handleCreatePipeline)
		r.With(s.perm(rbac.PermPipelinesRead)).Get("/{id}/report", s.handlePipelineReport)
	})

	// Deals
	r.Route("/deals", func(r chi.Router) {
		r.With(s.perm(rbac.PermDealsRead)).Get("/", s.handleListDeals)
		r.With(s.perm(rbac.PermDealsCreate)).Post("/", s.handleCreateDeal)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermDealsRead)).Get("/", s.handleGetDeal)
			r.With(s.perm(rbac.PermDealsUpdate)).Put("/", s.handleUpdateDeal)
			r.With(s.perm(rbac.PermDealsArchive)).Delete("/", s.handleArchiveDeal)
			r.With(s.perm(rbac.PermDealsChangeStage)).Post("/stage", s.handleMoveDealStage)
			r.With(s.perm(rbac.PermDealsRead)).Get("/history", s.handleDealHistory)
		})
	})

	// Tasks
	r.Route("/tasks", func(r chi.Router) {
		r.With(s.perm(rbac.PermTasksRead)).Get("/", s.handleListTasks)
		r.With(s.perm(rbac.PermTasksCreate)).Post("/", s.handleCreateTask)
		r.With(s.perm(rbac.PermTasksUpdate)).Put("/{id}", s.handleUpdateTask)
	})

	// Activities
	r.Route("/activities", func(r chi.Router) {
		r.With(s.perm(rbac.PermActivitiesRead)).Get("/", s.handleListActivities)
		r.With(s.perm(rbac.PermActivitiesCreate)).Post("/", s.handleCreateActivity)
	})

	// Tickets
	r.Route("/tickets", func(r chi.Router) {
		r.With(s.perm(rbac.PermTicketsRead)).Get("/", s.handleListTickets)
		r.With(s.perm(rbac.PermTicketsCreate)).Post("/", s.handleCreateTicket)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermTicketsRead)).Get("/", s.handleGetTicket)
			r.With(s.perm(rbac.PermTicketsUpdate)).Put("/", s.handleUpdateTicket)
		})
	})

	// Tags, comments, custom fields
	r.Route("/tags", func(r chi.Router) {
		r.With(s.perm(rbac.PermContactsRead)).Get("/", s.handleListTags)
		r.With(s.perm(rbac.PermTagsManage)).Post("/", s.handleCreateTag)
		r.With(s.perm(rbac.PermTagsManage)).Post("/{id}/attach", s.handleAttachTag)
	})
	r.Route("/comments", func(r chi.Router) {
		r.With(s.perm(rbac.PermContactsRead)).Get("/", s.handleListComments)
		r.With(s.perm(rbac.PermCommentsCreate)).Post("/", s.handleCreateComment)
	})
	r.Route("/custom-fields", func(r chi.Router) {
		r.With(s.perm(rbac.PermContactsRead)).Get("/", s.handleListCustomFields)
		r.With(s.perm(rbac.PermCustomFieldsManage)).Post("/", s.handleCreateCustomField)
	})

	// Notifications
	r.Route("/notifications", func(r chi.Router) {
		r.With(s.perm(rbac.PermNotificationsRead)).Get("/", s.handleListNotifications)
		r.With(s.perm(rbac.PermNotificationsRead)).Post("/{id}/read", s.handleMarkNotificationRead)
	})

	// Search and reports
	r.With(s.perm(rbac.PermContactsRead)).Get("/search", s.handleSearch)
	r.Route("/reports", func(r chi.Router) {
		r.With(s.perm(rbac.PermReportsRead)).Get("/sales-summary", s.handleSalesSummary)
		r.With(s.perm(rbac.PermReportsRead)).Get("/support-summary", s.handleSupportSummary)
	})
}

// listQueryFrom builds the common list query from the request.
func listQueryFrom(r *http.Request, allowed map[string]string, def httpx.SortSpec) crm.ListQuery {
	q := crm.ListQuery{
		Pagination:      httpx.ParsePagination(r),
		Sort:            httpx.ParseSort(r, allowed, def),
		Search:          r.URL.Query().Get("q"),
		Status:          r.URL.Query().Get("status"),
		IncludeArchived: queryBool(r, "include_archived", false),
	}
	if v := r.URL.Query().Get("owner_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			q.OwnerID = &id
		}
	}
	if v := r.URL.Query().Get("team_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			q.TeamID = &id
		}
	}
	if v := r.URL.Query().Get("account_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			q.AccountID = &id
		}
	}
	if v := r.URL.Query().Get("deal_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			q.DealID = &id
		}
	}
	if v := r.URL.Query().Get("assignee_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			q.AssigneeID = &id
		}
	}
	if v := r.URL.Query().Get("pipeline_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			q.PipelineID = &id
		}
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.From = &t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.To = &t
		}
	}
	return q
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

var accountSorts = map[string]string{
	"name": "name", "created_at": "created_at", "updated_at": "updated_at",
	"revenue": "annual_revenue", "employees": "employee_count",
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, accountSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	items, total, err := s.deps.CRM.ListAccounts(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.AccountInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	account, err := s.deps.CRM.CreateAccount(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, account)
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	account, err := s.deps.CRM.GetAccount(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, account)
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in crm.AccountInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	account, err := s.deps.CRM.UpdateAccount(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, account)
}

func (s *Server) handleArchiveAccount(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.CRM.ArchiveAccount(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "archived"})
}

func (s *Server) handleAccountContacts(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	q := listQueryFrom(r, contactSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	q.AccountID = &id
	items, total, err := s.deps.CRM.ListContacts(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleAccountDeals(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	q := listQueryFrom(r, dealSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	q.AccountID = &id
	items, total, err := s.deps.CRM.ListDeals(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleAccountActivities(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	q := listQueryFrom(r, activitySorts, httpx.SortSpec{Column: "occurred_at", Desc: true})
	q.AccountID = &id
	items, total, err := s.deps.CRM.ListActivities(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

// ---------------------------------------------------------------------------
// Contacts
// ---------------------------------------------------------------------------

var contactSorts = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at",
	"first_name": "first_name", "last_name": "last_name", "email": "email",
}

func (s *Server) handleListContacts(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, contactSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	items, total, err := s.deps.CRM.ListContacts(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateContact(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.ContactInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	contact, err := s.deps.CRM.CreateContact(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, contact)
}

func (s *Server) handleGetContact(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	contact, err := s.deps.CRM.GetContact(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, contact)
}

func (s *Server) handleUpdateContact(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in crm.ContactInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	contact, err := s.deps.CRM.UpdateContact(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, contact)
}

func (s *Server) handleArchiveContact(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.CRM.ArchiveContact(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "archived"})
}

// ---------------------------------------------------------------------------
// Leads
// ---------------------------------------------------------------------------

var leadSorts = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at",
	"title": "title", "status": "status", "score": "score",
}

func (s *Server) handleListLeads(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, leadSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	items, total, err := s.deps.CRM.ListLeads(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateLead(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.LeadInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	lead, err := s.deps.CRM.CreateLead(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, lead)
}

func (s *Server) handleGetLead(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	lead, err := s.deps.CRM.GetLead(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, lead)
}

func (s *Server) handleUpdateLead(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in crm.LeadInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	lead, err := s.deps.CRM.UpdateLead(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, lead)
}

func (s *Server) handleConvertLead(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	lead, account, contact, err := s.deps.CRM.ConvertLead(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"lead": lead, "account": account, "contact": contact})
}

// ---------------------------------------------------------------------------
// Pipelines and deals
// ---------------------------------------------------------------------------

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pipelines, err := s.deps.CRM.ListPipelines(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, pipelines)
}

type createPipelineRequest struct {
	Name     string              `json:"name"`
	Currency string              `json:"currency"`
	Stages   []crm.PipelineStage `json:"stages"`
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createPipelineRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pipeline, err := s.deps.CRM.CreatePipeline(r.Context(), p, req.Name, req.Currency, req.Stages)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, pipeline)
}

func (s *Server) handlePipelineReport(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	report, err := s.deps.CRM.PipelineReportFor(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, report)
}

var dealSorts = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at", "title": "title",
	"amount": "amount", "expected_close_date": "expected_close_date", "status": "status",
}

func (s *Server) handleListDeals(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, dealSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	items, total, err := s.deps.CRM.ListDeals(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateDeal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.DealInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	deal, err := s.deps.CRM.CreateDeal(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, deal)
}

func (s *Server) handleGetDeal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	deal, err := s.deps.CRM.GetDeal(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, deal)
}

func (s *Server) handleUpdateDeal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in crm.DealInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	deal, err := s.deps.CRM.UpdateDeal(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, deal)
}

func (s *Server) handleArchiveDeal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.CRM.ArchiveDeal(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "archived"})
}

type moveStageRequest struct {
	StageID string `json:"stage_id"`
	Note    string `json:"note"`
}

func (s *Server) handleMoveDealStage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req moveStageRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	stageID, ok := parseUUID(req.StageID)
	if !ok {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"stage_id": "must be a UUID"}))
		return
	}
	deal, err := s.deps.CRM.MoveDealStage(r.Context(), p, id, stageID, req.Note)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, deal)
}

func (s *Server) handleDealHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	history, err := s.deps.CRM.DealStageHistory(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, history)
}

// ---------------------------------------------------------------------------
// Tasks, activities, tickets
// ---------------------------------------------------------------------------

var taskSorts = map[string]string{
	"created_at": "created_at", "due_at": "due_at", "priority": "priority",
	"status": "status", "title": "title",
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, taskSorts, httpx.SortSpec{Column: "due_at", Desc: false})
	items, total, err := s.deps.CRM.ListTasks(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.TaskInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	task, err := s.deps.CRM.CreateTask(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in crm.TaskInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	task, err := s.deps.CRM.UpdateTask(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, task)
}

var activitySorts = map[string]string{
	"occurred_at": "occurred_at", "created_at": "created_at", "type": "type",
}

func (s *Server) handleListActivities(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, activitySorts, httpx.SortSpec{Column: "occurred_at", Desc: true})
	items, total, err := s.deps.CRM.ListActivities(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateActivity(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.ActivityInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	activity, err := s.deps.CRM.CreateActivity(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, activity)
}

var ticketSorts = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at", "priority": "priority",
	"status": "status", "subject": "subject", "number": "number",
}

func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, ticketSorts, httpx.SortSpec{Column: "created_at", Desc: true})
	items, total, err := s.deps.CRM.ListTickets(r.Context(), p, q)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, q.Pagination))
}

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in crm.TicketInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ticket, err := s.deps.CRM.CreateTicket(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, ticket)
}

func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ticket, err := s.deps.CRM.GetTicket(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, ticket)
}

func (s *Server) handleUpdateTicket(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in crm.TicketInput
	if err := decodeBody(w, r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ticket, err := s.deps.CRM.UpdateTicket(r.Context(), p, id, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, ticket)
}

// ---------------------------------------------------------------------------
// Tags, comments, custom fields, notifications, search, reports
// ---------------------------------------------------------------------------

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	tags, err := s.deps.CRM.ListTags(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, tags)
}

type createTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createTagRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tag, err := s.deps.CRM.CreateTag(r.Context(), p, req.Name, req.Color)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, tag)
}

type attachTagRequest struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

func (s *Server) handleAttachTag(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	tagID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req attachTagRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	entityID, ok := parseUUID(req.EntityID)
	if !ok {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"entity_id": "must be a UUID"}))
		return
	}
	if err := s.deps.CRM.TagEntity(r.Context(), p, tagID, req.EntityType, entityID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "tag attached"})
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	entityType := r.URL.Query().Get("entity_type")
	entityID, ok := parseUUID(r.URL.Query().Get("entity_id"))
	if !ok {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"entity_id": "must be a UUID"}))
		return
	}
	comments, err := s.deps.CRM.ListComments(r.Context(), p, entityType, entityID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, comments)
}

type createCommentRequest struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Body       string `json:"body"`
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createCommentRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	entityID, ok := parseUUID(req.EntityID)
	if !ok {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"entity_id": "must be a UUID"}))
		return
	}
	comment, err := s.deps.CRM.CreateComment(r.Context(), p, req.EntityType, entityID, req.Body)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, comment)
}

func (s *Server) handleListCustomFields(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	entityType := r.URL.Query().Get("entity_type")
	if entityType == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"entity_type": "is required"}))
		return
	}
	fields, err := s.deps.CRM.ListCustomFields(r.Context(), p, entityType)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, fields)
}

func (s *Server) handleCreateCustomField(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var def crm.CustomFieldDefinition
	if err := decodeBody(w, r, &def); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	created, err := s.deps.CRM.CreateCustomField(r.Context(), p, def)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, created)
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	unreadOnly := queryBool(r, "unread_only", false)
	items, total, err := s.deps.CRM.ListNotifications(r.Context(), p, unreadOnly, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.CRM.MarkNotificationRead(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "marked as read"})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	term := r.URL.Query().Get("q")
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parseInt(v); err == nil {
			limit = n
		}
	}
	results, err := s.deps.CRM.Search(r.Context(), p, term, limit)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	if results == nil {
		// Always return a JSON array so clients can iterate without a null check.
		results = []crm.SearchResult{}
	}
	httpx.WriteJSONList(w, results)
}

func (s *Server) handleSalesSummary(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	q := listQueryFrom(r, nil, httpx.SortSpec{})
	report, err := s.deps.CRM.SalesSummary(r.Context(), p, q.From, q.To)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, report)
}

func (s *Server) handleSupportSummary(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	report, err := s.deps.CRM.SupportSummary(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, report)
}

// parseUUID parses a UUID from a query string value.
func parseUUID(raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// parseInt parses an integer from a query string value.
func parseInt(raw string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(raw))
}
