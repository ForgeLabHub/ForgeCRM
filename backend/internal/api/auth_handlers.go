package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// handleHealth is a liveness probe: it reports that the process is running.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, map[string]any{
		"status":  "ok",
		"service": "forgecrm",
		"version": s.deps.Version,
	})
}

// handleReady is a readiness probe: it reports whether this instance can serve
// requests, which is a stricter question than "is the database reachable".
//
// It checks two things, in order, and reports the specific one that failed so a
// failing probe is diagnosable from its body:
//
//   - the database answers a ping;
//   - the schema the binary expects is present.
//
// The second check is the one that was missing. A reachable but un-migrated
// database passed the old probe, so an orchestrator routed traffic to an instance
// whose every query failed with `relation "users" does not exist`. Readiness now
// means "this process can actually answer requests", which is what the probe is
// for.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.deps.DB.Ping(ctx); err != nil {
		httpx.WriteJSONStatus(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable",
			"reason": "database is not reachable",
		})
		return
	}
	if err := migrate.Check(ctx, s.deps.DB.Pool()); err != nil {
		httpx.WriteJSONStatus(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable",
			"reason": "database schema is not ready",
			"detail": err.Error(),
		})
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"status":  "ready",
		"version": s.deps.Version,
		"commit":  s.deps.BuildCommit,
	})
}

// ---------------------------------------------------------------------------
// Login / logout
// ---------------------------------------------------------------------------

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	User        *authn.User `json:"user"`
	CSRFToken   string      `json:"csrf_token,omitempty"`
	MFARequired bool        `json:"mfa_required"`
	// MFATicket is returned only when a second factor is required, and can be
	// used only with /auth/mfa/verify.
	MFATicket string    `json:"mfa_ticket,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"email": "email and password are required"}))
		return
	}

	result, err := s.deps.Auth.Login(r.Context(), authn.LoginRequest{
		Email:     req.Email,
		Password:  req.Password,
		IP:        httpx.ClientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}

	if result.MFARequired {
		httpx.WriteJSON(w, loginResponse{
			User: result.User, MFARequired: true, MFATicket: result.MFATicket,
		})
		return
	}

	s.deps.Auth.SetSessionCookie(w, result.Token, result.Session.ExpiresAt)
	httpx.WriteJSON(w, loginResponse{
		User:      result.User,
		CSRFToken: result.Session.CSRFToken,
		ExpiresAt: result.Session.ExpiresAt,
	})
}

type mfaVerifyRequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

func (s *Server) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	var req mfaVerifyRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.Ticket == "" || req.Code == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"code": "ticket and code are required"}))
		return
	}
	sess, token, err := s.deps.Auth.VerifyMFA(r.Context(), req.Ticket, req.Code,
		httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	user, err := s.deps.Auth.LoadUser(r.Context(), sess.UserID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	s.deps.Auth.SetSessionCookie(w, token, sess.ExpiresAt)
	httpx.WriteJSON(w, loginResponse{
		User: user, CSRFToken: sess.CSRFToken, ExpiresAt: sess.ExpiresAt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.deps.Auth.SessionCookieName())
	if err == nil && cookie.Value != "" {
		if err := s.deps.Auth.Logout(r.Context(), cookie.Value); err != nil {
			s.deps.Log.Warn("logout failed", "error", err)
		}
	}
	if p, ok := principalFrom(r.Context()); ok {
		_ = s.deps.Audit.Record(r.Context(), audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionLogout), EntityType: "session",
			EntityID: p.SessionID.String(), IP: httpx.ClientIP(r),
		})
	}
	s.deps.Auth.ClearSessionCookie(w)
	httpx.WriteJSON(w, map[string]any{"status": "signed out"})
}

// meResponse describes the authenticated caller, including resolved permissions
// so the UI can hide what the user cannot do. The server still enforces every
// permission independently; this is convenience, not security.
type meResponse struct {
	User         *authn.User       `json:"user"`
	Organization orgSummary        `json:"organization"`
	Permissions  []rbac.Permission `json:"permissions"`
	Teams        []string          `json:"teams"`
	// CSRFToken lets a client that reloads the page keep making mutations
	// without signing in again. It is derived from the session secret, so it
	// grants nothing on its own: a caller still needs the session cookie, and
	// the token only proves the request did not originate from another site.
	CSRFToken string `json:"csrf_token,omitempty"`
}

type orgSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	user, err := s.deps.Auth.LoadUser(r.Context(), p.UserID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	var orgName string
	const q = `SELECT name FROM organizations WHERE id = $1`
	if err := s.deps.DB.Q(r.Context()).QueryRow(ctxOrBackground(r), q, p.OrganizationID).Scan(&orgName); err != nil {
		orgName = ""
	}
	teams := make([]string, 0, len(p.TeamIDs))
	for _, t := range p.TeamIDs {
		teams = append(teams, t.String())
	}
	// The session is resolved by middleware and carries the derived CSRF token.
	var csrfToken string
	if sess, ok := sessionFrom(r.Context()); ok && sess != nil {
		csrfToken = sess.CSRFToken
	}
	httpx.WriteJSON(w, meResponse{
		User:         user,
		Organization: orgSummary{ID: p.OrganizationID.String(), Name: orgName},
		Permissions:  p.Permissions(),
		Teams:        teams,
		CSRFToken:    csrfToken,
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	sessions, err := s.deps.Auth.ListSessions(r.Context(), p.UserID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	// Mark the caller's own session so the UI can label it.
	for _, sess := range sessions {
		if id, ok := sess["id"].(interface{ String() string }); ok {
			sess["current"] = id.String() == p.SessionID.String()
		}
	}
	httpx.WriteJSONList(w, sessions)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	// A user may only revoke their own sessions.
	const q = `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`
	tag, err := s.deps.DB.Q(r.Context()).Exec(ctxOrBackground(r), q, id, p.UserID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionSessionRevoked), EntityType: "session", EntityID: id.String(),
		IP: httpx.ClientIP(r),
	})
	httpx.WriteJSON(w, map[string]any{"status": "session revoked"})
}

// ---------------------------------------------------------------------------
// Password
// ---------------------------------------------------------------------------

type passwordForgotRequest struct {
	Email string `json:"email"`
}

// handlePasswordForgot always reports success so that the endpoint cannot be used
// to discover which addresses have accounts.
func (s *Server) handlePasswordForgot(w http.ResponseWriter, r *http.Request) {
	var req passwordForgotRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	token, userID, err := s.deps.Auth.RequestPasswordReset(r.Context(), req.Email)
	if err != nil {
		s.deps.Log.Error("password reset request failed", "error", err)
	}
	response := map[string]any{
		"status": "if an account exists for that address, a reset link has been sent",
	}
	// When email is not configured the token is returned so an operator can
	// complete the reset. This is safe only because it requires the SMTP
	// subsystem to be off, and it is never enabled in production.
	if token != "" && !s.deps.Email.Enabled() && !s.deps.Config.IsProduction() {
		response["reset_token"] = token
		response["note"] = "email is not configured, so the token is returned directly (development only)"
		// The response carries a one-time token, so it must not be kept by any
		// intermediary — nor by the idempotency layer, which reads this header.
		w.Header().Set("Cache-Control", "no-store")
	}
	if token != "" && s.deps.Email.Enabled() && userID != nil {
		link := s.deps.Config.BaseURL + "/reset-password?token=" + token
		go s.sendResetEmail(*userID, link)
	}
	httpx.WriteJSON(w, response)
}

func (s *Server) sendResetEmail(userID interface{ String() string }, link string) {
	// Deliberately detached from the request context: the response has already
	// been sent, and a slow SMTP server must not hold the request open.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var email string
	const q = `SELECT email FROM users WHERE id = $1`
	if err := s.deps.DB.Pool().QueryRow(ctx, q, userID).Scan(&email); err != nil {
		return
	}
	body := "<p>A password reset was requested for your ForgeCRM account.</p>" +
		`<p><a href="` + link + `">Set a new password</a></p>` +
		"<p>This link expires in one hour. If you did not request it, you can ignore this message.</p>"
	if err := s.deps.Email.Send(ctx, notifyMessage(email, "Reset your ForgeCRM password", body, "password_reset")); err != nil {
		s.deps.Log.Warn("could not send password reset email", "error", err)
	}
}

type passwordResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.Token == "" || req.NewPassword == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"new_password": "token and new_password are required"}))
		return
	}
	if _, err := s.deps.Auth.CompletePasswordReset(r.Context(), req.Token, req.NewPassword); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{
		"status": "password updated; all existing sessions have been signed out",
	})
}

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req passwordChangeRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	keep := p.SessionID
	if err := s.deps.Auth.ChangePassword(r.Context(), p.UserID, req.CurrentPassword,
		req.NewPassword, &keep); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = sess
	httpx.WriteJSON(w, map[string]any{"status": "password changed; other sessions have been signed out"})
}

// ---------------------------------------------------------------------------
// MFA
// ---------------------------------------------------------------------------

func (s *Server) handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	secret, url, err := s.deps.Auth.EnrollMFA(r.Context(), p.UserID, p.Email)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	// The secret is returned once, for the authenticator app. It is never logged.
	//
	// `no-store` keeps it that way through the idempotency layer as well: the
	// middleware that buffers a mutating response for replay reads this header and
	// refuses to write a response that carries it. Without the header the seed would
	// be persisted in plaintext and handed to a later request reusing the key.
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, map[string]any{
		"secret":      secret,
		"otpauth_url": url,
		"next_step":   "confirm the enrolment with /auth/mfa/confirm and a current code",
	})
}

type mfaConfirmRequest struct {
	Code string `json:"code"`
}

func (s *Server) handleMFAConfirm(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req mfaConfirmRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	codes, err := s.deps.Auth.ConfirmMFA(r.Context(), p.UserID, req.Code)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionMFAEnabled), EntityType: "user", EntityID: p.UserID.String(),
	})
	httpx.WriteJSON(w, map[string]any{
		"status":         "multi-factor authentication is enabled",
		"recovery_codes": codes,
		"note":           "store these recovery codes securely; each can be used once",
	})
}

func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	if err := s.deps.Auth.DisableMFA(r.Context(), p.UserID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionMFADisabled), EntityType: "user", EntityID: p.UserID.String(),
	})
	httpx.WriteJSON(w, map[string]any{"status": "multi-factor authentication is disabled"})
}
