package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// uuidType is the UUID type used by the AI principal adapter.
type uuidType = uuid.UUID

// mountNotifications registers email, Telegram and notification routes.
func (s *Server) mountNotifications(r chi.Router) {
	r.Route("/admin/email", func(r chi.Router) {
		r.With(s.perm(rbac.PermEmailManage)).Get("/status", s.handleEmailStatus)
		r.With(s.perm(rbac.PermEmailManage)).Post("/test", s.handleEmailTest)
		r.With(s.perm(rbac.PermEmailManage)).Post("/send", s.handleEmailSend)
		r.With(s.perm(rbac.PermNotificationsRead)).Get("/log", s.handleEmailLog)
	})
	r.Route("/admin/telegram", func(r chi.Router) {
		r.With(s.perm(rbac.PermEmailManage)).Get("/status", s.handleTelegramStatus)
		r.With(s.perm(rbac.PermEmailManage)).Post("/link-code", s.handleTelegramLinkCode)
		r.With(s.perm(rbac.PermEmailManage)).Delete("/link", s.handleTelegramUnlink)
	})
}

func (s *Server) handleEmailStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"enabled": s.deps.Email.Enabled(),
	}
	if !s.deps.Email.Enabled() {
		status["reason"] = "SMTP is not configured; the platform works normally without email"
		httpx.WriteJSON(w, status)
		return
	}
	// ValidateConfig and TestConnection are reported separately so "the settings
	// are consistent" is never confused with "the server accepted our credentials".
	if err := s.deps.Email.ValidateConfig(); err != nil {
		status["config_valid"] = false
		status["config_error"] = err.Error()
		httpx.WriteJSON(w, status)
		return
	}
	status["config_valid"] = true
	if err := s.deps.Email.TestConnection(r.Context()); err != nil {
		status["reachable"] = false
		status["error"] = err.Error()
	} else {
		status["reachable"] = true
		status["note"] = "the server is reachable and credentials were accepted; this is not proof that a message will be delivered"
	}
	httpx.WriteJSON(w, status)
}

// handleEmailTest sends a real message to the caller's own address.
//
// Sending to the caller is deliberate: a test must not deliver mail to an
// arbitrary third party.
func (s *Server) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	if !s.deps.Email.Enabled() {
		httpx.WriteError(w, r, httpx.NewError(503, httpx.CodeDisabled, "SMTP is not configured"))
		return
	}
	body := "<p>This is a test message from ForgeCRM.</p><p>If you received it, outbound email is working.</p>"

	if err := s.deps.Email.Send(r.Context(), notifyMessage(p.Email,
		"ForgeCRM test message", body, "smtp_test")); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: "notifications.email_test_sent", EntityType: "email", EntityID: p.Email,
	})
	httpx.WriteJSON(w, map[string]any{"status": "the server accepted the message for delivery"})
}

type sendEmailRequest struct {
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	// Purpose is what the message is about. It defaults to marketing, the stricter
	// category, so a caller that omits it gets the safe rule rather than an accidental
	// pass: an operator sending a bulk mail must have a recorded grant, and one sending
	// a receipt says so explicitly.
	Purpose string `json:"purpose"`
}

func (s *Server) handleEmailSend(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req sendEmailRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if len(req.To) == 0 || req.Subject == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"to": "to and subject are required"}))
		return
	}
	purpose := consent.Purpose(req.Purpose)
	if req.Purpose == "" {
		purpose = consent.PurposeMarketing
	}
	// Consent is checked here, at the one point a manual message leaves the platform,
	// against the recipient's address. The check is not optional and does not depend on
	// the caller remembering it: a blocked send is refused with 403 and the reason, and
	// the refusal is audited by Enforce. Transactional mail is not gated; marketing is.
	decision, err := s.deps.Consent.Enforce(r.Context(), p.OrganizationID, &p.UserID,
		consent.SubjectForEmail(req.To[0]), purpose, consent.ChannelEmail)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	if !decision.Allowed {
		httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden,
			"consent blocks this send: "+decision.Reason))
		return
	}
	// The transport check comes after the consent answer. A message consent forbids
	// is refused with 403 whether or not SMTP is configured; reporting "SMTP is not
	// configured" would imply the send would succeed if it were.
	if !s.deps.Email.Enabled() {
		httpx.WriteError(w, r, httpx.NewError(503, httpx.CodeDisabled, "SMTP is not configured"))
		return
	}
	if err := s.deps.Email.Send(r.Context(), notifyMessage(req.To[0], req.Subject, req.Body, "manual")); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: "notifications.email_sent_manual", EntityType: "email", EntityID: req.To[0],
		Metadata: map[string]any{"purpose": string(purpose)},
	})
	httpx.WriteJSON(w, map[string]any{"status": "the server accepted the message for delivery"})
}

func (s *Server) handleEmailLog(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	items, total, err := s.deps.Email.ListSendLog(r.Context(), &p.OrganizationID, pg.Limit, pg.Offset)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, httpx.NewListResponse(items, total, pg))
}

func (s *Server) handleTelegramStatus(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, map[string]any{
		"enabled":  s.deps.Telegram.Enabled(),
		"reason":   reasonIfDisabled(s.deps.Telegram.Enabled(), "no Telegram bot token is configured; the platform works normally without it"),
		"commands": []string{"/start", "/link", "/tasks", "/unlink", "/help"},
	})
}

func reasonIfDisabled(enabled bool, reason string) string {
	if enabled {
		return ""
	}
	return reason
}

func (s *Server) handleTelegramLinkCode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	code, err := s.deps.Telegram.CreateLinkCode(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"code":               code,
		"expires_in_seconds": int((15 * time.Minute).Seconds()),
		"instruction":        "send /link " + code + " to the bot",
	})
}

func (s *Server) handleTelegramUnlink(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	if err := s.deps.Telegram.Unlink(r.Context(), p); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "Telegram is no longer linked to your account"})
}
