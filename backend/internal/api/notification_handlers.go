package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/notify"
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
	// The purpose is what selects the rule, so it must be one the platform knows.
	// An unrecognised value is refused here rather than passed to the policy: a
	// caller that wrote "newsletter" would otherwise be asking a question the model
	// cannot answer, and a policy that guessed would be guessing in the permissive
	// direction for a marketing message.
	if !purpose.Valid() {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"purpose": "is not a supported purpose"}))
		return
	}
	// Consent is enforced by the transport, not here. The check a handler performs is
	// the check the next caller can forget, and one already did: the queued-send worker
	// reached SMTP without asking. The transport therefore asks for every recipient
	// before it opens a connection, and this handler inherits that answer. The purpose
	// and the acting user travel with the message so the policy is asked the same
	// question it would be asked for a queued send.
	//
	// The transport check also lives in Send, and the ordering is preserved there: a
	// message consent forbids is refused with 403 whether or not SMTP is configured,
	// because an answer of "SMTP is not configured" would imply the send would succeed
	// once mail is set up.
	msg := notify.Message{
		To:          req.To,
		Subject:     req.Subject,
		HTMLBody:    req.Body,
		Template:    "manual",
		OrgID:       &p.OrganizationID,
		Purpose:     string(purpose),
		ActorUserID: &p.UserID,
	}
	if err := s.deps.Email.Send(r.Context(), msg); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: "notifications.email_sent_manual", EntityType: "email", EntityID: strings.Join(req.To, ","),
		Metadata: map[string]any{"purpose": string(purpose), "recipients": len(req.To)},
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
