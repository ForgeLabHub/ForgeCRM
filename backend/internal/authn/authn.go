// Package authn implements authentication: password login, opaque sessions,
// CSRF, TOTP MFA, account lockout and password reset.
//
// # Session model
//
// A session is an opaque 256-bit random token. The browser holds the token in an
// HttpOnly cookie; the database holds only its SHA-256 hash. The token is never
// exposed to JavaScript and never placed in localStorage. Each session also has
// an independent random secret from which its CSRF token is derived, so a CSRF
// token is only valid for the session it was issued to.
//
// Session fixation is prevented by issuing a brand-new token on login and on MFA
// verification, and by discarding the pre-authentication session row.
package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// Errors returned by the service. Handlers map these to HTTP statuses.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountLocked      = errors.New("account is temporarily locked")
	ErrAccountInactive    = errors.New("account is not active")
	ErrSessionInvalid     = errors.New("session is invalid or expired")
	ErrMFARequired        = errors.New("multi-factor verification required")
	ErrMFAInvalid         = errors.New("invalid multi-factor code")
	ErrCSRF               = errors.New("csrf token missing or invalid")
	ErrResetTokenInvalid  = errors.New("password reset token is invalid or expired")
	ErrWeakPassword       = errors.New("password does not meet the minimum requirements")
)

// MinPasswordLength is the minimum accepted password length.
const MinPasswordLength = 12

// User is the authenticated account record.
type User struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Status          string     `json:"status"`
	IsPlatformOwner bool       `json:"is_platform_owner"`
	MFAEnabled      bool       `json:"mfa_enabled"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
}

// Session is a resolved session.
type Session struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	CSRFToken   string
	ExpiresAt   time.Time
	MFAVerified bool
	// StepUpAt is when this session last presented a second factor, if ever. A
	// destructive decision requires it to be recent; nil means never.
	StepUpAt *time.Time
}

// StepUpWindow is how long a second-factor presentation remains valid for a
// destructive action. It is deliberately short: the point of the step-up is that
// the operator is present and authenticated now, not that they authenticated at
// some point during a long session.
const StepUpWindow = 15 * time.Minute

// SteppedUp reports whether the session presented a second factor within the
// window.
func (s *Session) SteppedUp() bool {
	if s == nil || s.StepUpAt == nil {
		return false
	}
	return time.Since(*s.StepUpAt) <= StepUpWindow
}

// Service performs authentication operations.
type Service struct {
	db      *database.DB
	cfg     *config.Config
	keyring *secrets.Keyring
	audit   *audit.Logger
	rbac    *rbac.Service
}

// NewService builds the authentication service.
func NewService(db *database.DB, cfg *config.Config, keyring *secrets.Keyring, auditLog *audit.Logger, rbacSvc *rbac.Service) *Service {
	return &Service{db: db, cfg: cfg, keyring: keyring, audit: auditLog, rbac: rbacSvc}
}

// ---------------------------------------------------------------------------
// Password policy
// ---------------------------------------------------------------------------

// ValidatePassword enforces the password policy. It deliberately avoids
// composition rules (which push users toward predictable substitutions) and
// instead requires length plus a check against the most common passwords.
func ValidatePassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return fmt.Errorf("%w: at least %d characters are required", ErrWeakPassword, MinPasswordLength)
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("%w: password must not be blank", ErrWeakPassword)
	}
	lower := strings.ToLower(password)
	for _, weak := range commonPasswords {
		if lower == weak {
			return fmt.Errorf("%w: this password is too common", ErrWeakPassword)
		}
	}
	return nil
}

var commonPasswords = []string{
	"password1234", "password12345", "administrator", "letmein12345",
	"qwertyuiop12", "123456789012", "changeme1234", "welcome12345",
	"forgecrm1234", "admin1234567",
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

// LoginRequest carries the credentials for a login attempt.
type LoginRequest struct {
	Email     string
	Password  string
	IP        string
	UserAgent string
}

// LoginResult is returned on a successful credential check.
type LoginResult struct {
	User        *User
	Session     *Session
	Token       string // raw session token, set only when MFA is not required
	MFARequired bool
	// MFATicket is a short-lived, single-purpose token allowing only the MFA
	// verification step; it cannot be used as a session.
	MFATicket string
}

// Login verifies credentials and, when MFA is not enabled, creates a session.
//
// Failures are intentionally indistinguishable to the caller (no "user not
// found" versus "wrong password"), while the audit and security logs record the
// real reason for the operator.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResult, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		return nil, ErrInvalidCredentials
	}

	var (
		id           uuid.UUID
		passwordHash *string
		status       string
		isOwner      bool
		mfaEnabled   bool
		lockedUntil  *time.Time
		displayName  string
	)
	const q = `
		SELECT id, password_hash, status, is_platform_owner, mfa_enabled,
		       locked_until, display_name
		FROM users WHERE email = $1`

	err := s.db.Q(ctx).QueryRow(ctx, q, email).Scan(
		&id, &passwordHash, &status, &isOwner, &mfaEnabled, &lockedUntil, &displayName)

	if errors.Is(err, pgx.ErrNoRows) {
		// Spend comparable time on a missing account so that response timing does
		// not disclose whether the address exists.
		_, _ = secrets.HashPassword(req.Password)
		s.recordLoginAttempt(ctx, email, req.IP, false, "unknown_user")
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			Type: "auth.login_failed", Severity: "warn",
			Message:  "login attempted for unknown account",
			Metadata: map[string]any{"email": email, "reason": "unknown_user"},
			IP:       req.IP, UserAgent: req.UserAgent,
		})
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}

	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		s.recordLoginAttempt(ctx, email, req.IP, false, "locked")
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			OrganizationID: nil, UserID: audit.UUIDPtr(id),
			Type: "auth.login_blocked", Severity: "warn",
			Message:  "login attempt on locked account",
			Metadata: map[string]any{"reason": "locked"},
			IP:       req.IP, UserAgent: req.UserAgent,
		})
		return nil, ErrAccountLocked
	}

	if status == "blocked" || status == "suspended" {
		s.recordLoginAttempt(ctx, email, req.IP, false, "inactive:"+status)
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			UserID: audit.UUIDPtr(id), Type: "auth.login_blocked", Severity: "warn",
			Message:  "login attempt on inactive account",
			Metadata: map[string]any{"reason": status},
			IP:       req.IP, UserAgent: req.UserAgent,
		})
		return nil, ErrAccountInactive
	}

	if passwordHash == nil || *passwordHash == "" {
		// Accounts created by invitation or SSO have no local password yet.
		s.recordLoginAttempt(ctx, email, req.IP, false, "no_local_password")
		return nil, ErrInvalidCredentials
	}

	if err := secrets.VerifyPassword(req.Password, *passwordHash); err != nil {
		attempts, lockUntil, uErr := s.registerFailedAttempt(ctx, id)
		if uErr != nil {
			return nil, uErr
		}
		s.recordLoginAttempt(ctx, email, req.IP, false, "bad_password")
		sev := "warn"
		if lockUntil != nil {
			sev = "error"
		}
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			UserID: audit.UUIDPtr(id), Type: "auth.login_failed", Severity: sev,
			Message: "invalid password",
			Metadata: map[string]any{
				"failed_attempts": attempts,
				"locked":          lockUntil != nil,
			},
			IP: req.IP, UserAgent: req.UserAgent,
		})
		return nil, ErrInvalidCredentials
	}

	// Credentials are valid. Clear the failure counter.
	if _, err := s.db.Q(ctx).Exec(ctx,
		`UPDATE users SET failed_attempts = 0, locked_until = NULL WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("reset failed attempts: %w", err)
	}
	s.recordLoginAttempt(ctx, email, req.IP, true, "")

	user := &User{
		ID: id, Email: email, DisplayName: displayName, Status: status,
		IsPlatformOwner: isOwner, MFAEnabled: mfaEnabled,
	}

	if mfaEnabled {
		ticket, err := s.issueMFATicket(ctx, id)
		if err != nil {
			return nil, err
		}
		return &LoginResult{User: user, MFARequired: true, MFATicket: ticket}, nil
	}

	sess, token, err := s.createSession(ctx, id, req.IP, req.UserAgent, true)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("record last login: %w", err)
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(id), Type: "auth.login_succeeded", Severity: "info",
		Message: "login succeeded", IP: req.IP, UserAgent: req.UserAgent,
	})
	return &LoginResult{User: user, Session: sess, Token: token}, nil
}

func (s *Service) recordLoginAttempt(ctx context.Context, email, ip string, success bool, reason string) {
	const q = `
		INSERT INTO login_attempts (email, ip, success, reason)
		VALUES ($1, NULLIF($2, '')::inet, $3, NULLIF($4, ''))`
	_, _ = s.db.Q(ctx).Exec(ctx, q, email, ip, success, reason)
}

// registerFailedAttempt increments the failure counter for one user and returns
// the new count together with the lock expiry, which is set on the attempt that
// reaches the threshold.
//
// The increment is a single statement so it is atomic in the database. The
// obvious Go-side read-modify-write is not: two replicas both read N, both write
// N+1, and one failure is lost. With a threshold of T that hands an attacker
// more than T attempts per window, and the excess grows with the replica count —
// exactly when the control is supposed to matter. A concurrency test in
// lockout_integration_test.go lost 34 of 40 attempts against the previous code.
//
// The lock is expressed as "this increment reached the threshold", not as a
// separate comparison against a value read earlier, so two replicas racing to
// lock converge on the same expiry instead of one overwriting the other's.
//
// A window that has already elapsed is treated as a fresh start: the counter
// resets rather than continuing from the previous burst, so a user who mistyped
// twice yesterday and twice today is not locked out.
//
// Both assignments are written as expressions over the row's own columns, with no
// CTE and no read taken earlier in the statement. Under READ COMMITTED, when the
// UPDATE finds the row already changed by a concurrent transaction, Postgres
// re-evaluates every SET expression against the new version; a CTE or a value
// read before the update is not re-evaluated and would be written back stale,
// which is what lost 2 of 40 attempts in the first version of this fix. The
// reset is repeated inline in the lock decision so both expressions see the same
// value.
func (s *Service) registerFailedAttempt(ctx context.Context, userID uuid.UUID) (int, *time.Time, error) {
	const q = `
		UPDATE users
		   SET failed_attempts = CASE
		           WHEN locked_until IS NOT NULL AND locked_until <= now() THEN 1
		           ELSE failed_attempts + 1
		       END,
		       locked_until = CASE
		           WHEN locked_until IS NOT NULL AND locked_until > now() THEN locked_until
		           WHEN $2 > 0 AND (CASE
		                    WHEN locked_until IS NOT NULL AND locked_until <= now() THEN 1
		                    ELSE failed_attempts + 1
		                END) >= $2
		           THEN now() + ($3 * interval '1 millisecond')
		           ELSE NULL
		       END
		 WHERE id = $1
		 RETURNING failed_attempts, locked_until`

	var (
		attempts    int
		lockedUntil *time.Time
	)
	if err := s.db.Q(ctx).QueryRow(ctx, q, userID, s.cfg.LockoutThreshold, s.cfg.LockoutWindow.Milliseconds()).
		Scan(&attempts, &lockedUntil); err != nil {
		return 0, nil, fmt.Errorf("register failed attempt: %w", err)
	}
	return attempts, lockedUntil, nil
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// createSession inserts a session row and returns the raw token. Only the token
// hash is persisted.
func (s *Service) createSession(ctx context.Context, userID uuid.UUID, ip, userAgent string, mfaVerified bool) (*Session, string, error) {
	token, err := secrets.RandomToken(32)
	if err != nil {
		return nil, "", err
	}
	csrfSecret, err := secrets.RandomToken(32)
	if err != nil {
		return nil, "", err
	}
	expires := time.Now().Add(s.cfg.SessionTTL)

	var sess Session
	// step_up_at is set here because both callers have just presented a factor:
	// the password, plus a TOTP code when MFA is enabled. A destructive decision
	// made within StepUpWindow of this is therefore backed by a factor presented
	// now; one made from an older session is refused until the operator steps up
	// again.
	const q = `
		INSERT INTO sessions (user_id, token_hash, csrf_secret, expires_at, ip, user_agent, mfa_verified, step_up_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, $6, $7, now())
		RETURNING id, expires_at, step_up_at`
	var stepUpAt time.Time
	err = s.db.Q(ctx).QueryRow(ctx, q,
		userID, secrets.HashToken(token), []byte(csrfSecret), expires,
		ip, userAgent, mfaVerified).Scan(&sess.ID, &sess.ExpiresAt, &stepUpAt)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}
	sess.UserID = userID
	sess.MFAVerified = mfaVerified
	sess.StepUpAt = &stepUpAt
	sess.CSRFToken = s.keyring.DeriveCSRF([]byte(csrfSecret))
	return &sess, token, nil
}

// StepUp refreshes a session's second-factor timestamp after the operator has
// presented a factor again.
//
// This is what makes the destructive tier's requirement enforceable over the life
// of a session: a session that has been open longer than the window must present a
// factor again before it can authorise a destructive action. The timestamp is
// updated in the database rather than returned to the caller, so the decision is
// durable and cannot be replayed by a client that simply claims it stepped up.
func (s *Service) StepUp(ctx context.Context, sessionID uuid.UUID) error {
	const q = `UPDATE sessions SET step_up_at = now() WHERE id = $1 AND revoked_at IS NULL`
	tag, err := s.db.Q(ctx).Exec(ctx, q, sessionID)
	if err != nil {
		return fmt.Errorf("record step-up: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionInvalid
	}
	return nil
}

// ResolveSession validates a raw session token and returns the session.
//
// Expired and revoked sessions are treated identically to unknown ones. A
// session that has not completed MFA verification is rejected for general use.
func (s *Service) ResolveSession(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrSessionInvalid
	}
	var (
		sess        Session
		csrfSecret  []byte
		revokedAt   *time.Time
		mfaVerified bool
		stepUpAt    *time.Time
	)
	const q = `
		SELECT id, user_id, csrf_secret, expires_at, revoked_at, mfa_verified, step_up_at
		FROM sessions WHERE token_hash = $1`
	err := s.db.Q(ctx).QueryRow(ctx, q, secrets.HashToken(token)).Scan(
		&sess.ID, &sess.UserID, &csrfSecret, &sess.ExpiresAt, &revokedAt, &mfaVerified, &stepUpAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("resolve session: %w", err)
	}
	if revokedAt != nil || !sess.ExpiresAt.After(time.Now()) {
		return nil, ErrSessionInvalid
	}
	if !mfaVerified {
		return nil, ErrMFARequired
	}
	sess.CSRFToken = s.keyring.DeriveCSRF(csrfSecret)
	sess.MFAVerified = true
	sess.StepUpAt = stepUpAt
	return &sess, nil
}

// TouchSession updates last_seen_at. Failures are not fatal to the request.
func (s *Service) TouchSession(ctx context.Context, sessionID uuid.UUID) {
	const q = `UPDATE sessions SET last_seen_at = now() WHERE id = $1`
	_, _ = s.db.Q(ctx).Exec(ctx, q, sessionID)
}

// RevokeSession marks a session revoked. Idempotent.
func (s *Service) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	const q = `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`
	if _, err := s.db.Q(ctx).Exec(ctx, q, sessionID); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// RevokeAllSessions revokes every active session for a user, except optionally
// one that should be kept (the caller's own session).
func (s *Service) RevokeAllSessions(ctx context.Context, userID uuid.UUID, keep *uuid.UUID) (int64, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	if keep != nil {
		const q = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`
		t, e := s.db.Q(ctx).Exec(ctx, q, userID, *keep)
		tag, err = t, e
	} else {
		const q = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
		t, e := s.db.Q(ctx).Exec(ctx, q, userID)
		tag, err = t, e
	}
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListSessions returns a user's active sessions.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]map[string]any, error) {
	const q = `
		SELECT id, created_at, expires_at, last_seen_at, host(ip), user_agent, mfa_verified
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY last_seen_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id         uuid.UUID
			createdAt  time.Time
			expiresAt  time.Time
			lastSeenAt time.Time
			ip         *string
			ua         *string
			mfa        bool
		)
		if err := rows.Scan(&id, &createdAt, &expiresAt, &lastSeenAt, &ip, &ua, &mfa); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "created_at": createdAt, "expires_at": expiresAt,
			"last_seen_at": lastSeenAt, "ip": ip, "user_agent": ua, "mfa_verified": mfa,
		})
	}
	return out, rows.Err()
}

// LoadUser returns the user record for a session.
func (s *Service) LoadUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	var u User
	const q = `
		SELECT id, email, display_name, status, is_platform_owner, mfa_enabled, last_login_at
		FROM users WHERE id = $1`
	err := s.db.Q(ctx).QueryRow(ctx, q, userID).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.IsPlatformOwner, &u.MFAEnabled, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	// A blocked or suspended user loses access immediately, regardless of any
	// still-valid session row.
	if u.Status == "blocked" || u.Status == "suspended" {
		return nil, ErrAccountInactive
	}
	return &u, nil
}

// ---------------------------------------------------------------------------
// CSRF
// ---------------------------------------------------------------------------

// VerifyCSRF compares a presented CSRF token against the session's expected one.
func (s *Service) VerifyCSRF(sess *Session, provided string) error {
	if sess == nil || provided == "" {
		return ErrCSRF
	}
	expected := []byte(sess.CSRFToken)
	got := []byte(provided)
	if len(expected) != len(got) || !secrets.ConstantTimeEqual(expected, got) {
		return ErrCSRF
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cookies
// ---------------------------------------------------------------------------

// SetSessionCookie writes the session cookie with hardened attributes.
func (s *Service) SetSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionCookieName exposes the configured cookie name.
func (s *Service) SessionCookieName() string { return s.cfg.SessionCookieName }

// ---------------------------------------------------------------------------
// MFA tickets
// ---------------------------------------------------------------------------

// MFATicketTTL is how long a user has to complete the second factor.
const MFATicketTTL = 5 * time.Minute

// issueMFATicket creates a session row that is valid only for the MFA step. It
// is stored with mfa_verified = false, which ResolveSession rejects for normal
// use, so the ticket cannot be replayed as a session.
func (s *Service) issueMFATicket(ctx context.Context, userID uuid.UUID) (string, error) {
	token, err := secrets.RandomToken(32)
	if err != nil {
		return "", err
	}
	csrfSecret, err := secrets.RandomToken(32)
	if err != nil {
		return "", err
	}
	const q = `
		INSERT INTO sessions (user_id, token_hash, csrf_secret, expires_at, mfa_verified)
		VALUES ($1, $2, $3, $4, false)`
	if _, err := s.db.Q(ctx).Exec(ctx, q,
		userID, secrets.HashToken(token), []byte(csrfSecret),
		time.Now().Add(MFATicketTTL)); err != nil {
		return "", fmt.Errorf("create mfa ticket: %w", err)
	}
	return token, nil
}

// VerifyMFA completes the second factor and promotes the ticket to a session.
func (s *Service) VerifyMFA(ctx context.Context, ticket, code, ip, userAgent string) (*Session, string, error) {
	var (
		sessID      uuid.UUID
		userID      uuid.UUID
		expiresAt   time.Time
		revokedAt   *time.Time
		mfaVerified bool
	)
	const q = `
		SELECT id, user_id, expires_at, revoked_at, mfa_verified
		FROM sessions WHERE token_hash = $1`
	err := s.db.Q(ctx).QueryRow(ctx, q, secrets.HashToken(ticket)).Scan(
		&sessID, &userID, &expiresAt, &revokedAt, &mfaVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrSessionInvalid
	}
	if err != nil {
		return nil, "", fmt.Errorf("load mfa ticket: %w", err)
	}
	if revokedAt != nil || !expiresAt.After(time.Now()) || mfaVerified {
		return nil, "", ErrSessionInvalid
	}

	ok, err := s.verifyTOTPOrRecovery(ctx, userID, code)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			UserID: audit.UUIDPtr(userID), Type: "auth.mfa_failed", Severity: "warn",
			Message: "invalid multi-factor code", IP: ip, UserAgent: userAgent,
		})
		return nil, "", ErrMFAInvalid
	}

	// Retire the ticket and issue a fresh session token: a new token on
	// privilege elevation prevents fixation.
	if err := s.RevokeSession(ctx, sessID); err != nil {
		return nil, "", err
	}
	sess, token, err := s.createSession(ctx, userID, ip, userAgent, true)
	if err != nil {
		return nil, "", err
	}
	if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
		return nil, "", fmt.Errorf("record last login: %w", err)
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.mfa_succeeded", Severity: "info",
		Message: "multi-factor verification succeeded", IP: ip, UserAgent: userAgent,
	})
	return sess, token, nil
}

// StepUpFactor re-verifies a factor for the session's user and, on success,
// refreshes the session's step-up timestamp.
//
// The factor depends on how the account is protected. With MFA enabled it is a
// TOTP or recovery code, which is a true second factor. Without MFA the best
// available factor is the password, re-presented: weaker, but still proof that the
// person at the keyboard knows a secret the session cookie alone does not carry.
// The distinction is recorded in the audit entry so an auditor can see which kind
// of factor backed a destructive decision.
//
// A failed attempt is audited as a security event but does not lock the account:
// the endpoint is already behind a session and a rate limit, and locking on a
// mistyped code would let an attacker with a stolen cookie deny the real user
// access.
func (s *Service) StepUpFactor(ctx context.Context, sessionID, userID uuid.UUID, code, password string) error {
	var mfaEnabled bool
	var hash *string
	const q = `SELECT mfa_enabled, password_hash FROM users WHERE id = $1 AND status = 'active'`
	if err := s.db.Q(ctx).QueryRow(ctx, q, userID).Scan(&mfaEnabled, &hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("load user for step-up: %w", err)
	}

	kind := "password"
	ok := false
	if mfaEnabled {
		kind = "totp"
		var err error
		ok, err = s.verifyTOTPOrRecovery(ctx, userID, code)
		if err != nil {
			return err
		}
	} else {
		if hash == nil || *hash == "" {
			return ErrInvalidCredentials
		}
		ok = secrets.VerifyPassword(password, *hash) == nil
	}

	if !ok {
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			UserID: audit.UUIDPtr(userID), Type: "auth.step_up_failed", Severity: "warn",
			Message: "step-up verification failed", Metadata: map[string]any{"factor": kind},
		})
		return ErrInvalidCredentials
	}

	if err := s.StepUp(ctx, sessionID); err != nil {
		return err
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.step_up_succeeded", Severity: "info",
		Message: "step-up verification succeeded", Metadata: map[string]any{"factor": kind},
	})
	return nil
}

// verifyTOTPOrRecovery accepts either a TOTP code or an unused recovery code.
func (s *Service) verifyTOTPOrRecovery(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return false, nil
	}
	var secret string
	const q = `SELECT secret FROM mfa_factors WHERE user_id = $1 AND type = 'totp' AND confirmed_at IS NOT NULL`
	err := s.db.Q(ctx).QueryRow(ctx, q, userID).Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load mfa factor: %w", err)
	}
	if totp.Validate(code, secret) {
		return true, nil
	}
	// Fall back to a recovery code. A recovery code is single-use.
	hash := secrets.HashToken(strings.ToUpper(code))
	const use = `
		UPDATE recovery_codes SET used_at = now()
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
		RETURNING id`
	var id uuid.UUID
	err = s.db.Q(ctx).QueryRow(ctx, use, userID, hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.recovery_code_used", Severity: "warn",
		Message: "a recovery code was used to complete sign-in",
	})
	return true, nil
}

// EnrollMFA creates a TOTP factor for the user and returns the provisioning URI.
// The factor is unconfirmed until ConfirmMFA is called, so a user who abandons
// enrolment does not lock themselves out.
func (s *Service) EnrollMFA(ctx context.Context, userID uuid.UUID, email string) (secret, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "ForgeCRM",
		AccountName: email,
		Period:      30,
		SecretSize:  32,
		Algorithm:   otp.AlgorithmSHA1,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp secret: %w", err)
	}
	const q = `
		INSERT INTO mfa_factors (user_id, type, secret, confirmed_at)
		VALUES ($1, 'totp', $2, NULL)
		ON CONFLICT (user_id, type) DO UPDATE SET secret = EXCLUDED.secret, confirmed_at = NULL`
	if _, err := s.db.Q(ctx).Exec(ctx, q, userID, key.Secret()); err != nil {
		return "", "", fmt.Errorf("store totp secret: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// ConfirmMFA activates an enrolled factor after verifying a first code, and
// issues fresh recovery codes.
func (s *Service) ConfirmMFA(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	var secret string
	const q = `SELECT secret FROM mfa_factors WHERE user_id = $1 AND type = 'totp' AND confirmed_at IS NULL`
	if err := s.db.Q(ctx).QueryRow(ctx, q, userID).Scan(&secret); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMFAInvalid
		}
		return nil, fmt.Errorf("load pending factor: %w", err)
	}
	if !totp.Validate(code, secret) {
		return nil, ErrMFAInvalid
	}

	var plainCodes []string
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const confirm = `UPDATE mfa_factors SET confirmed_at = now() WHERE user_id = $1 AND type = 'totp'`
		if _, err := s.db.Q(ctx).Exec(ctx, confirm, userID); err != nil {
			return fmt.Errorf("confirm factor: %w", err)
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("clear recovery codes: %w", err)
		}
		for i := 0; i < 10; i++ {
			raw, err := secrets.RandomToken(9)
			if err != nil {
				return err
			}
			code := strings.ToUpper(raw)
			plainCodes = append(plainCodes, code)
			const ins = `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2)`
			if _, err := s.db.Q(ctx).Exec(ctx, ins, userID, secrets.HashToken(code)); err != nil {
				return fmt.Errorf("store recovery code: %w", err)
			}
		}
		const flag = `UPDATE users SET mfa_enabled = true WHERE id = $1`
		if _, err := s.db.Q(ctx).Exec(ctx, flag, userID); err != nil {
			return fmt.Errorf("enable mfa flag: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.mfa_enabled", Severity: "info",
		Message: "multi-factor authentication enabled",
	})
	return plainCodes, nil
}

// DisableMFA removes the factor and recovery codes.
func (s *Service) DisableMFA(ctx context.Context, userID uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM mfa_factors WHERE user_id = $1`, userID); err != nil {
			return err
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
			return err
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE users SET mfa_enabled = false WHERE id = $1`, userID); err != nil {
			return err
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Password reset
// ---------------------------------------------------------------------------

// ResetTokenTTL is how long a password reset link is valid.
const ResetTokenTTL = 60 * time.Minute

// RequestPasswordReset creates a single-use, hashed reset token.
//
// It always reports success, whether or not the address exists, so that the
// endpoint cannot be used to enumerate accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) (string, *uuid.UUID, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var userID uuid.UUID
	const q = `SELECT id FROM users WHERE email = $1 AND status <> 'blocked'`
	err := s.db.Q(ctx).QueryRow(ctx, q, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("find user for reset: %w", err)
	}
	token, err := secrets.RandomToken(32)
	if err != nil {
		return "", nil, err
	}
	const ins = `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`
	if _, err := s.db.Q(ctx).Exec(ctx, ins, userID,
		secrets.HashToken(token), time.Now().Add(ResetTokenTTL)); err != nil {
		return "", nil, fmt.Errorf("store reset token: %w", err)
	}
	return token, &userID, nil
}

// CompletePasswordReset consumes a reset token and sets a new password. Every
// existing session is revoked, which is the expected behaviour after a credential
// compromise.
func (s *Service) CompletePasswordReset(ctx context.Context, token, newPassword string) (uuid.UUID, error) {
	if err := ValidatePassword(newPassword); err != nil {
		return uuid.Nil, err
	}
	hash, err := secrets.HashPassword(newPassword)
	if err != nil {
		return uuid.Nil, err
	}
	var userID uuid.UUID
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const sel = `
			UPDATE password_reset_tokens SET used_at = now()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
			RETURNING user_id`
		if err := s.db.Q(ctx).QueryRow(ctx, sel, secrets.HashToken(token)).Scan(&userID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrResetTokenInvalid
			}
			return fmt.Errorf("consume reset token: %w", err)
		}
		const upd = `
			UPDATE users SET password_hash = $2, failed_attempts = 0, locked_until = NULL,
			                 status = CASE WHEN status = 'invited' THEN 'active' ELSE status END
			WHERE id = $1`
		if _, err := s.db.Q(ctx).Exec(ctx, upd, userID, hash); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		const revoke = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
		if _, err := s.db.Q(ctx).Exec(ctx, revoke, userID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.password_reset_completed", Severity: "warn",
		Message: "password was reset; all sessions revoked",
	})
	return userID, nil
}

// ChangePassword verifies the current password and sets a new one, revoking
// other sessions.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string, keepSession *uuid.UUID) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	var hash *string
	const q = `SELECT password_hash FROM users WHERE id = $1`
	if err := s.db.Q(ctx).QueryRow(ctx, q, userID).Scan(&hash); err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	if hash == nil || *hash == "" {
		return ErrInvalidCredentials
	}
	if err := secrets.VerifyPassword(currentPassword, *hash); err != nil {
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			UserID: audit.UUIDPtr(userID), Type: "auth.password_change_failed", Severity: "warn",
			Message: "password change attempted with an incorrect current password",
		})
		return ErrInvalidCredentials
	}
	newHash, err := secrets.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if _, err := s.db.Q(ctx).Exec(ctx,
		`UPDATE users SET password_hash = $2 WHERE id = $1`, userID, newHash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if _, err := s.RevokeAllSessions(ctx, userID, keepSession); err != nil {
		return err
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.password_changed", Severity: "warn",
		Message: "password changed; other sessions revoked",
	})
	return nil
}

// Logout revokes the session identified by the raw token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	const q = `UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`
	if _, err := s.db.Q(ctx).Exec(ctx, q, secrets.HashToken(token)); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// CorrelationContext attaches the correlation id for audit records raised inside
// background jobs, where there is no HTTP request.
func CorrelationContext(ctx context.Context) context.Context {
	return logging.WithCorrelationID(ctx, uuid.NewString())
}
