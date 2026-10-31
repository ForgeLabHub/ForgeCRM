package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountConsent registers the consent routes.
//
// Consent is addressed by a subject, which is either a contact id or an email
// address, so the reads live under a query parameter rather than a path segment
// that could be either. Recording a decision and reading the history are separate
// permissions: the first changes whether a person can be contacted, the second is
// how an operator or auditor finds out why.
func (s *Server) mountConsent(r chi.Router) {
	r.Route("/consent", func(r chi.Router) {
		r.Use(s.requireService(func() bool { return s.deps.Consent != nil }, "consent"))
		r.With(s.perm(rbac.PermConsentRead)).Get("/", s.handleListConsent)
		r.With(s.perm(rbac.PermConsentRead)).Get("/current", s.handleCurrentConsent)
		r.With(s.perm(rbac.PermConsentRead)).Get("/history", s.handleConsentHistory)
		r.With(s.perm(rbac.PermConsentRead)).Get("/decide", s.handleDecideConsent)
		r.With(s.perm(rbac.PermConsentManage)).Post("/", s.handleRecordConsent)
		r.With(s.perm(rbac.PermConsentManage)).Post("/withdraw", s.handleWithdrawConsent)
	})
}

// subjectFromRequest reads the subject a request is about. Exactly one of
// contact_id or email must be present; the service refuses an ambiguous or empty
// subject, so this only parses.
func subjectFromRequest(r *http.Request) (consent.Subject, error) {
	q := r.URL.Query()
	contactID := q.Get("contact_id")
	email := q.Get("email")
	switch {
	case contactID != "" && email != "":
		return consent.Subject{}, &consent.ValidationError{Fields: map[string]string{
			"subject": "pass contact_id or email, not both",
		}}
	case contactID != "":
		id, ok := parseUUID(contactID)
		if !ok {
			return consent.Subject{}, &consent.ValidationError{Fields: map[string]string{
				"contact_id": "must be a UUID",
			}}
		}
		return consent.SubjectForContact(id), nil
	case email != "":
		return consent.SubjectForEmail(email), nil
	default:
		return consent.Subject{}, &consent.ValidationError{Fields: map[string]string{
			"subject": "pass contact_id or email",
		}}
	}
}

func (s *Server) handleListConsent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	subject, err := subjectFromRequest(r)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	items, err := s.deps.Consent.List(r.Context(), p, subject)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

func (s *Server) handleCurrentConsent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	subject, err := subjectFromRequest(r)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	items, err := s.deps.Consent.Current(r.Context(), p, subject)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

func (s *Server) handleConsentHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	subject, err := subjectFromRequest(r)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	q := r.URL.Query()
	purpose := consent.Purpose(q.Get("purpose"))
	channel := consent.Channel(q.Get("channel"))
	items, err := s.deps.Consent.History(r.Context(), p, subject, purpose, channel)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

// handleDecide answers whether a subject may be contacted, and why. It is the same
// decision an outbound channel makes, exposed so an operator can ask the question
// directly instead of inferring the answer from a list.
func (s *Server) handleDecideConsent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	subject, err := subjectFromRequest(r)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	q := r.URL.Query()
	decision, err := s.deps.Consent.Decide(r.Context(), p.OrganizationID, subject,
		consent.Purpose(q.Get("purpose")), consent.Channel(q.Get("channel")))
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, decision)
}

func (s *Server) handleRecordConsent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in consent.Input
	if err := httpx.DecodeJSON(w, r, &in, 16*1024); err != nil {
		return
	}
	rec, err := s.deps.Consent.Record(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, rec)
}

type withdrawConsentRequest struct {
	ContactID *string `json:"contact_id"`
	Email     string  `json:"email"`
	Purpose   string  `json:"purpose"`
	Channel   string  `json:"channel"`
	Reason    string  `json:"reason"`
}

func (s *Server) handleWithdrawConsent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req withdrawConsentRequest
	if err := httpx.DecodeJSON(w, r, &req, 16*1024); err != nil {
		return
	}
	var subject consent.Subject
	switch {
	case req.ContactID != nil && *req.ContactID != "":
		id, ok := parseUUID(*req.ContactID)
		if !ok {
			httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
				"contact_id": "must be a UUID"}))
			return
		}
		subject = consent.SubjectForContact(id)
	case req.Email != "":
		subject = consent.SubjectForEmail(req.Email)
	default:
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"subject": "pass contact_id or email"}))
		return
	}
	rec, err := s.deps.Consent.Withdraw(r.Context(), p, subject,
		consent.Purpose(req.Purpose), consent.Channel(req.Channel), req.Reason)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, rec)
}
