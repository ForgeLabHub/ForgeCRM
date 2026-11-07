// Package oidc implements corporate single sign-on over OpenID Connect.
//
// # What this is
//
// An authorization-code flow with PKCE and a nonce, against any standards
// compliant identity provider. Nothing here is tied to a particular vendor: the
// provider is described by its issuer URL, and the endpoints are read from the
// provider's discovery document.
//
// # Trust boundaries
//
//   - The issuer and the redirect URL are operator configuration, so they are
//     trusted the way any deployment setting is. The client secret is stored only
//     as ciphertext under the platform keyring.
//   - Everything the provider asserts about a person arrives as an ID token. The
//     signature, the issuer, the audience, the expiry and the nonce are all
//     verified before any claim is read. See provider.go.
//   - An email claim is only used to find an existing account when the provider
//     says the address is verified and the address is inside the configured
//     domains. Otherwise a provider that lets a user set an arbitrary unverified
//     address would be an account-takeover path.
//   - A local account is identified by (provider, subject), never by email. Email
//     addresses change and providers recycle them.
//
// # Sessions
//
// This package authenticates a person but does not mint a session. It returns the
// resolved local account and the caller (internal/api) issues the session through
// internal/authn, so there is exactly one place that creates sessions.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// Errors returned by the service. Handlers map these to HTTP statuses.
var (
	// ErrDisabled means SSO is not configured on this deployment.
	ErrDisabled = errors.New("single sign-on is not enabled")
	// ErrUnknownProvider means no enabled provider matches the requested key.
	ErrUnknownProvider = errors.New("unknown or disabled identity provider")
	// ErrStateInvalid means the authorization response does not match any live
	// request: expired, already used, or forged.
	ErrStateInvalid = errors.New("the sign-in request is invalid or has expired")
	// ErrTokenInvalid means the ID token failed verification.
	ErrTokenInvalid = errors.New("the identity provider response failed verification")
	// ErrNoAccount means the provider authenticated the person but no local account
	// is linked and just-in-time provisioning is off.
	ErrNoAccount = errors.New("no account is linked to this identity; sign in another way and link it, or ask an administrator to invite you")
	// ErrEmailNotAllowed means the asserted address is outside the provider's
	// permitted domains, or is not verified.
	ErrEmailNotAllowed = errors.New("the identity provider did not assert a permitted, verified email address")
	// ErrIdentityAlreadyLinked means the provider subject is already bound to a
	// local account, so it cannot be bound to a second one.
	ErrIdentityAlreadyLinked = errors.New("this identity is already linked to an account")
	// ErrIdentityInUse means the caller's account is already bound to a different
	// subject at this provider, so a second link would be ambiguous.
	ErrIdentityInUse = errors.New("this account already has an identity at this provider")
	// ErrIdentityNotFound means the caller has no such link to remove.
	ErrIdentityNotFound = errors.New("no such linked identity")
	// ErrAccountExists means a local account already carries the asserted
	// address, so just-in-time provisioning cannot create a second one. The
	// address is not used to attach the subject to that account; binding an
	// identity to an existing account is an explicit, separate act.
	ErrAccountExists = errors.New("an account with this email address already exists; sign in another way and link this identity to it")
	// ErrProviderUnreachable means discovery or the token exchange failed. It is
	// distinct from a bad token so the interface can offer a retry.
	ErrProviderUnreachable = errors.New("the identity provider could not be reached")
	// ErrInvalidProvider means the provider definition is not usable.
	ErrInvalidProvider = errors.New("the identity provider configuration is invalid")
	// ErrProviderNotInScope means the provider exists but belongs to another
	// tenant, so the caller may not read or change it. It is deliberately the same
	// error as an unknown provider so a caller cannot probe for the existence of a
	// provider in an organization it does not act in.
	ErrProviderNotInScope = errors.New("unknown or disabled identity provider")
)

// BindingError marks a failure that happened while explicitly linking a provider
// identity to an already-signed-in account. The callback uses it to send the
// browser back to the Security Center, where the binding was started, rather
// than to the sign-in page.
type BindingError struct{ Err error }

func (e *BindingError) Error() string { return e.Err.Error() }
func (e *BindingError) Unwrap() error { return e.Err }

// IsBindingError reports whether err was raised while binding an identity.
func IsBindingError(err error) bool {
	var be *BindingError
	return errors.As(err, &be)
}

// StateTTL bounds how long an authorization request may stay open.
const StateTTL = 10 * time.Minute

var (
	keyPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
	roleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
)

// Provider is one configured identity provider. ClientSecret is populated only
// when the record is read for the flow or the administration interface; it is
// never serialised to a client.
type Provider struct {
	ID                  uuid.UUID
	OrganizationID      *uuid.UUID
	Key                 string
	DisplayName         string
	Issuer              string
	ClientID            string
	ClientSecret        string
	Scopes              []string
	AllowedEmailDomains []string
	JITProvisioning     bool
	DefaultRoleKey      string
	Enabled             bool
}

// Public returns a copy safe to serialise: no secret, no issuer internals beyond
// what a client needs to render a sign-in button.
func (p Provider) Public() PublicProvider {
	return PublicProvider{
		ID:              p.ID,
		Key:             p.Key,
		DisplayName:     p.DisplayName,
		OrganizationID:  p.OrganizationID,
		JITProvisioning: p.JITProvisioning,
	}
}

// PublicProvider is the shape exposed to unauthenticated clients so they can
// render a sign-in button.
type PublicProvider struct {
	ID              uuid.UUID  `json:"id"`
	Key             string     `json:"key"`
	DisplayName     string     `json:"display_name"`
	OrganizationID  *uuid.UUID `json:"organization_id,omitempty"`
	JITProvisioning bool       `json:"jit_provisioning"`
}

// Identity is the result of a completed sign-in: the local account the provider
// authenticated, plus what was done to arrive at it.
type Identity struct {
	UserID         uuid.UUID
	OrganizationID *uuid.UUID
	Email          string
	DisplayName    string
	ProviderID     uuid.UUID
	ProviderKey    string
	// Provisioned is true when this sign-in created the local account.
	Provisioned bool
	// Bound is true when this sign-in attached the provider subject to the
	// already-signed-in account that requested the binding.
	Bound bool
	// LinkIdentityID names the user_identities row a Bound result created, so the
	// caller can report exactly what was linked.
	LinkIdentityID uuid.UUID
	// IdentityExists is true when the provider subject was already linked to a
	// local account. The sign-in succeeded for that account.
	IdentityExists bool
}

// Service performs the authorization-code flow and resolves the local account.
type Service struct {
	db      *database.DB
	cfg     *config.Config
	keyring *secrets.Keyring
	audit   *audit.Logger
	log     *slog.Logger

	// providers caches discovered provider metadata by issuer. Discovery is a
	// network round trip, and it changes rarely, so repeating it on every sign-in
	// would add latency and a failure mode for no benefit.
	mu        sync.Mutex
	providers map[string]cachedProvider
}

type cachedProvider struct {
	provider  *oidcProvider
	fetchedAt time.Time
}

// providerCacheTTL bounds how long a discovery document is reused.
const providerCacheTTL = 15 * time.Minute

// NewService builds the SSO service.
func NewService(db *database.DB, cfg *config.Config, keyring *secrets.Keyring,
	auditLog *audit.Logger, log *slog.Logger) *Service {
	return &Service{
		db:        db,
		cfg:       cfg,
		keyring:   keyring,
		audit:     auditLog,
		log:       log,
		providers: map[string]cachedProvider{},
	}
}

// Enabled reports whether SSO is configured at all.
func (s *Service) Enabled() bool {
	return s != nil && s.cfg != nil && s.cfg.OIDC.Enabled
}

// ProviderScope bounds which providers an administrative caller may read or
// mutate. A platform owner passes the zero value and sees platform-wide
// providers; everyone else is confined to their own tenant.
type ProviderScope struct {
	OrgID           uuid.UUID
	IsPlatformOwner bool
}

// allows reports whether a provider falls inside the caller's administrative
// scope: a platform-wide provider for a platform owner, or a provider owned by
// the caller's own organization. A tenant administrator never reaches a
// platform-wide provider through this path, because a mutation that changed the
// provider every tenant signs in through is not theirs to make.
func (sc ProviderScope) allows(p *Provider) bool {
	if sc.IsPlatformOwner {
		return p.OrganizationID == nil
	}
	return p.OrganizationID != nil && *p.OrganizationID == sc.OrgID
}

// ---------------------------------------------------------------------------
// Provider storage
// ---------------------------------------------------------------------------

// ProviderInput is the writable form of a provider.
type ProviderInput struct {
	OrganizationID *uuid.UUID
	Key            string
	DisplayName    string
	Issuer         string
	ClientID       string
	// ClientSecret is plaintext. On update, empty means "keep the stored secret".
	ClientSecret        string
	Scopes              []string
	AllowedEmailDomains []string
	JITProvisioning     bool
	DefaultRoleKey      string
	Enabled             bool
}

const providerColumns = `
	id, organization_id, key, display_name, issuer, client_id,
	client_secret_encrypted, scopes, allowed_email_domains,
	jit_provisioning, COALESCE(default_role_key, ''), enabled`

// scanProvider reads a provider row, decrypting the client secret.
func (s *Service) scanProvider(row pgx.Row) (*Provider, error) {
	var (
		p         Provider
		encSecret []byte
	)
	if err := row.Scan(&p.ID, &p.OrganizationID, &p.Key, &p.DisplayName, &p.Issuer,
		&p.ClientID, &encSecret, &p.Scopes, &p.AllowedEmailDomains,
		&p.JITProvisioning, &p.DefaultRoleKey, &p.Enabled); err != nil {
		return nil, err
	}
	if len(encSecret) > 0 {
		plain, err := s.keyring.Decrypt(encSecret)
		if err != nil {
			return nil, fmt.Errorf("decrypt the client secret for provider %q: %w", p.Key, err)
		}
		p.ClientSecret = string(plain)
	}
	return &p, nil
}

// List returns the providers visible to an organization: its own plus the
// platform-wide ones. A nil organization returns only the platform-wide set.
func (s *Service) List(ctx context.Context, orgID *uuid.UUID) ([]Provider, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if orgID == nil {
		rows, err = s.db.Q(ctx).Query(ctx,
			`SELECT `+providerColumns+` FROM oidc_providers WHERE organization_id IS NULL ORDER BY key`)
	} else {
		rows, err = s.db.Q(ctx).Query(ctx,
			`SELECT `+providerColumns+` FROM oidc_providers
			 WHERE organization_id IS NULL OR organization_id = $1 ORDER BY key`, *orgID)
	}
	if err != nil {
		return nil, fmt.Errorf("list identity providers: %w", err)
	}
	defer rows.Close()

	var out []Provider
	for rows.Next() {
		p, err := s.scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("read identity provider: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// EnabledProviders returns the enabled providers a signed-out visitor may choose
// from. It exposes no secrets.
func (s *Service) EnabledProviders(ctx context.Context) ([]PublicProvider, error) {
	if !s.Enabled() {
		return nil, nil
	}
	rows, err := s.db.Q(ctx).Query(ctx,
		`SELECT `+providerColumns+` FROM oidc_providers WHERE enabled ORDER BY display_name`)
	if err != nil {
		return nil, fmt.Errorf("list enabled identity providers: %w", err)
	}
	defer rows.Close()

	var out []PublicProvider
	for rows.Next() {
		p, err := s.scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("read identity provider: %w", err)
		}
		out = append(out, p.Public())
	}
	return out, rows.Err()
}

// GetByID loads one provider.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*Provider, error) {
	row := s.db.Q(ctx).QueryRow(ctx,
		`SELECT `+providerColumns+` FROM oidc_providers WHERE id = $1`, id)
	p, err := s.scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnknownProvider
	}
	if err != nil {
		return nil, fmt.Errorf("load identity provider: %w", err)
	}
	return p, nil
}

// resolveByKey finds the enabled provider a sign-in button referred to. An
// organization-scoped provider wins over a platform-wide one with the same key,
// so a tenant can override a shared definition.
func (s *Service) resolveByKey(ctx context.Context, key string) (*Provider, error) {
	row := s.db.Q(ctx).QueryRow(ctx,
		`SELECT `+providerColumns+` FROM oidc_providers
		 WHERE key = $1 AND enabled
		 ORDER BY (organization_id IS NULL), created_at
		 LIMIT 1`, key)
	p, err := s.scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnknownProvider
	}
	if err != nil {
		return nil, fmt.Errorf("resolve identity provider: %w", err)
	}
	return p, nil
}

// scopePredicate renders the SQL fragment that restricts a mutation to the rows
// the scope may touch, plus its bind arguments. Placeholder numbering continues
// from n so the caller can append it to a statement that already binds n-1
// values. A platform owner is confined to platform-wide rows (organization_id IS
// NULL); everyone else is confined to their own organization.
func scopePredicate(sc ProviderScope, n int) (string, []any) {
	if sc.IsPlatformOwner {
		return "organization_id IS NULL", nil
	}
	return fmt.Sprintf("organization_id = $%d", n), []any{sc.OrgID}
}

// Create stores a provider. A tenant administrator can only create a provider in
// their own organization: the scope overrides whatever the request body asked
// for, so a caller cannot plant a provider in a tenant it does not act in. A
// platform owner creates the platform-wide provider (organization_id NULL), which
// is the one every tenant signs in through.
func (s *Service) Create(ctx context.Context, in ProviderInput, scope ProviderScope, actor uuid.UUID) (*Provider, error) {
	// The scope decides ownership outright; the request body's organization is
	// ignored so a tenant administrator cannot plant a provider elsewhere. It is
	// set before validation, which checks that JIT provisioning is tenant-scoped.
	if scope.IsPlatformOwner {
		in.OrganizationID = nil
	} else {
		tenant := scope.OrgID
		in.OrganizationID = &tenant
	}
	if err := validateProviderInput(in); err != nil {
		return nil, err
	}
	var enc []byte
	if in.ClientSecret != "" {
		sealed, err := s.keyring.Encrypt([]byte(in.ClientSecret))
		if err != nil {
			return nil, fmt.Errorf("seal the client secret: %w", err)
		}
		enc = sealed
	}
	row := s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO oidc_providers (
			organization_id, key, display_name, issuer, client_id,
			client_secret_encrypted, scopes, allowed_email_domains,
			jit_provisioning, default_role_key, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12)
		RETURNING `+providerColumns,
		in.OrganizationID, in.Key, in.DisplayName, in.Issuer, in.ClientID,
		enc, defaultScopes(in.Scopes), nonNilTextArray(in.AllowedEmailDomains),
		in.JITProvisioning, in.DefaultRoleKey, in.Enabled, actor)
	p, err := s.scanProvider(row)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a provider with key %q already exists in this scope", ErrInvalidProvider, in.Key)
		}
		return nil, fmt.Errorf("create identity provider: %w", err)
	}
	return p, nil
}

// Update changes a provider. An empty ClientSecret leaves the stored one in
// place, so an operator can change the display name without re-entering a secret
// they cannot read back. The scope is part of the WHERE clause, so a caller who
// names a provider belonging to another tenant updates nothing and is told it is
// unknown rather than that it is forbidden.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in ProviderInput, scope ProviderScope, actor uuid.UUID) (*Provider, error) {
	if err := validateProviderInput(in); err != nil {
		return nil, err
	}
	var enc []byte
	if in.ClientSecret != "" {
		sealed, err := s.keyring.Encrypt([]byte(in.ClientSecret))
		if err != nil {
			return nil, fmt.Errorf("seal the client secret: %w", err)
		}
		enc = sealed
	}
	pred, predArgs := scopePredicate(scope, 11)
	args := append([]any{
		id, in.DisplayName, in.Issuer, in.ClientID, enc,
		defaultScopes(in.Scopes), nonNilTextArray(in.AllowedEmailDomains),
		in.JITProvisioning, in.DefaultRoleKey, in.Enabled,
	}, predArgs...)
	row := s.db.Q(ctx).QueryRow(ctx, `
		UPDATE oidc_providers SET
			display_name = $2,
			issuer = $3,
			client_id = $4,
			client_secret_encrypted = COALESCE($5, client_secret_encrypted),
			scopes = $6,
			allowed_email_domains = $7,
			jit_provisioning = $8,
			default_role_key = NULLIF($9,''),
			enabled = $10
		WHERE id = $1 AND `+pred+`
		RETURNING `+providerColumns,
		args...)
	p, err := s.scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrProviderNotInScope
	}
	if err != nil {
		return nil, fmt.Errorf("update identity provider: %w", err)
	}
	return p, nil
}

// Delete removes a provider. Linked identities and in-flight states cascade. The
// scope is part of the WHERE clause for the same reason as Update: a caller
// cannot delete a provider outside its tenant.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, scope ProviderScope) error {
	pred, predArgs := scopePredicate(scope, 2)
	args := append([]any{id}, predArgs...)
	tag, err := s.db.Q(ctx).Exec(ctx,
		`DELETE FROM oidc_providers WHERE id = $1 AND `+pred, args...)
	if err != nil {
		return fmt.Errorf("delete identity provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProviderNotInScope
	}
	return nil
}

// LinkedIdentity is a user_identities row as shown to the account holder.
type LinkedIdentity struct {
	ID           uuid.UUID `json:"id"`
	ProviderID   uuid.UUID `json:"provider_id"`
	ProviderKey  string    `json:"provider_key"`
	ProviderName string    `json:"provider_name"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at"`
}

// ListIdentities returns the provider links for an account, oldest first so the
// original link reads first in the interface.
func (s *Service) ListIdentities(ctx context.Context, userID uuid.UUID) ([]LinkedIdentity, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT i.id, i.provider_id, p.key, p.display_name, i.subject,
		       COALESCE(i.email, ''), i.created_at, i.last_login_at
		FROM user_identities i
		JOIN oidc_providers p ON p.id = i.provider_id
		WHERE i.user_id = $1
		ORDER BY i.created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	out := make([]LinkedIdentity, 0, 4)
	for rows.Next() {
		var li LinkedIdentity
		if err := rows.Scan(&li.ID, &li.ProviderID, &li.ProviderKey, &li.ProviderName,
			&li.Subject, &li.Email, &li.CreatedAt, &li.LastLoginAt); err != nil {
			return nil, fmt.Errorf("read identity: %w", err)
		}
		out = append(out, li)
	}
	return out, rows.Err()
}

func defaultScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"openid", "email", "profile"}
	}
	return scopes
}

// nonNilTextArray turns a nil slice into an empty one.
//
// A nil []string is encoded as SQL NULL, and these columns are NOT NULL with an
// empty-array default. Passing nil therefore fails the constraint rather than
// taking the default, which is what an omitted list should mean.
func nonNilTextArray(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func validateProviderInput(in ProviderInput) error {
	if strings.TrimSpace(in.Key) == "" || !keyPattern.MatchString(in.Key) {
		return fmt.Errorf("provider key must start with a letter and contain only lowercase letters, digits and underscores")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return fmt.Errorf("display name is required")
	}
	u, err := url.Parse(strings.TrimSpace(in.Issuer))
	if err != nil || u.Host == "" {
		return fmt.Errorf("issuer must be an absolute URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("issuer must use http or https")
	}
	if strings.TrimSpace(in.ClientID) == "" {
		return fmt.Errorf("client id is required")
	}
	if in.JITProvisioning && in.OrganizationID == nil {
		return fmt.Errorf("just-in-time provisioning requires an organization-scoped provider")
	}
	if in.DefaultRoleKey != "" && !roleKeyPattern.MatchString(in.DefaultRoleKey) {
		return fmt.Errorf("default role key is malformed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Authorization-code flow
// ---------------------------------------------------------------------------

// Begin starts an authorization request and returns the URL to send the browser
// to. The state, nonce and PKCE verifier are stored server-side; the browser
// carries only the opaque state.
//
// linkUserID is empty for an ordinary sign-in. When it is set, the request is an
// explicit binding: the callback attaches the resulting subject to that account
// instead of resolving a different one. The caller must have authenticated as that
// user; the service re-reads the account on the callback so a deleted user cannot
// be linked.
func (s *Service) Begin(ctx context.Context, providerKey, returnTo string, linkUserID uuid.UUID) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	provider, err := s.resolveByKey(ctx, providerKey)
	if err != nil {
		return "", err
	}

	state, err := randomString(32)
	if err != nil {
		return "", err
	}
	nonce, err := randomString(32)
	if err != nil {
		return "", err
	}
	verifier := generateVerifier()

	// A return target is a path inside this application, never an absolute URL, so
	// the callback cannot be turned into an open redirect.
	safeReturn := SafeReturnTo(returnTo)

	var linkArg any
	if linkUserID != uuid.Nil {
		linkArg = linkUserID
	}

	const q = `
		INSERT INTO oidc_login_states (state, provider_id, nonce, code_verifier, return_to, link_user_id, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`
	if _, err := s.db.Q(ctx).Exec(ctx, q, state, provider.ID, nonce, verifier,
		safeReturn, linkArg, time.Now().Add(StateTTL)); err != nil {
		return "", fmt.Errorf("record the sign-in request: %w", err)
	}

	cfg := s.oauthConfig(ctx, provider)
	return cfg.AuthCodeURL(state, nonceOption(nonce), s256ChallengeOption(verifier)), nil
}

// Complete finishes an authorization request: it exchanges the code, verifies the
// ID token, and resolves the local account. It does not create a session.
func (s *Service) Complete(ctx context.Context, code, state, ip, userAgent string) (*Identity, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	if code == "" || state == "" {
		return nil, ErrStateInvalid
	}

	// Claim the state row atomically. A second use of the same state finds no row,
	// which closes the replay window on a stolen authorization response.
	var (
		providerID uuid.UUID
		nonce      string
		verifier   string
		linkUserID *uuid.UUID
	)
	const claimQ = `
		UPDATE oidc_login_states
		SET consumed_at = now()
		WHERE state = $1 AND consumed_at IS NULL AND expires_at > now()
		RETURNING provider_id, nonce, code_verifier, link_user_id`
	err := s.db.Q(ctx).QueryRow(ctx, claimQ, state).Scan(&providerID, &nonce, &verifier, &linkUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStateInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("claim the sign-in request: %w", err)
	}

	provider, err := s.GetByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !provider.Enabled {
		return nil, ErrUnknownProvider
	}

	cfg := s.oauthConfig(ctx, provider)
	token, err := cfg.Exchange(ctx, code, verifierOption(verifier))
	if err != nil {
		s.logProviderFailure(provider, "token exchange", err)
		return nil, fmt.Errorf("%w", ErrProviderUnreachable)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("%w: the response carried no id_token", ErrTokenInvalid)
	}

	tokenVerifier, err := s.idTokenVerifier(ctx, provider)
	if err != nil {
		return nil, err
	}
	idToken, err := tokenVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.audit.RecordSecurityAsync(audit.SecurityEvent{
			Type: "auth.sso_token_rejected", Severity: "warn",
			Message:  "an ID token failed verification",
			Metadata: map[string]any{"provider": provider.Key},
			IP:       ip, UserAgent: userAgent,
		})
		return nil, fmt.Errorf("%w", ErrTokenInvalid)
	}
	if idToken.Nonce != nonce {
		return nil, fmt.Errorf("%w: nonce mismatch", ErrTokenInvalid)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		PreferredName string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: unreadable claims", ErrTokenInvalid)
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email != "" && !domainAllowed(email, provider.AllowedEmailDomains) {
		return nil, fmt.Errorf("%w: the address is outside the configured domains", ErrEmailNotAllowed)
	}

	displayName := firstNonEmpty(claims.Name, claims.PreferredName, email)
	if linkUserID != nil {
		id, err := s.bindIdentity(ctx, provider, *linkUserID, idToken.Subject, email, displayName, ip, userAgent)
		if err != nil {
			// Tag it so the callback returns the browser to the Security Center where the
			// binding started, not to the sign-in page.
			return nil, &BindingError{Err: err}
		}
		return id, nil
	}
	return s.resolveIdentity(ctx, provider, idToken.Subject, email, claims.EmailVerified, displayName, ip, userAgent)
}

// resolveIdentity finds the local account for a verified subject.
//
// The lookup is by subject alone. An address is never used to attach a subject to
// an account: the provider asserts the address, and a provider that lets a user
// choose an arbitrary verified address would then hand them whatever local account
// already carries it. Binding an identity to an existing account is a separate,
// explicit act — see bindIdentity.
func (s *Service) resolveIdentity(ctx context.Context, provider *Provider, subject, email string,
	emailVerified bool, displayName, ip, userAgent string) (*Identity, error) {

	// 1. An existing link is authoritative and needs no email at all.
	var (
		userID uuid.UUID
		orgID  *uuid.UUID
	)
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT user_id FROM user_identities WHERE provider_id = $1 AND subject = $2`,
		provider.ID, subject).Scan(&userID)
	if err == nil {
		if _, err := s.db.Q(ctx).Exec(ctx,
			`UPDATE user_identities SET last_login_at = now(), email = NULLIF($3,'')
			 WHERE provider_id = $1 AND subject = $2`, provider.ID, subject, email); err != nil {
			return nil, fmt.Errorf("record the sign-in on the identity: %w", err)
		}
		if err := s.db.Q(ctx).QueryRow(ctx,
			`SELECT organization_id FROM memberships WHERE user_id = $1 AND status = 'active'
			 ORDER BY is_org_owner DESC, created_at LIMIT 1`, userID).Scan(&orgID); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("resolve the account organization: %w", err)
			}
		}
		return &Identity{
			UserID: userID, OrganizationID: orgID, Email: email, DisplayName: displayName,
			ProviderID: provider.ID, ProviderKey: provider.Key, IdentityExists: true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("look up the linked identity: %w", err)
	}

	// 2. No link. Provisioning needs a verified address because the new account is
	// created with it, and an unverified one is attacker-controlled.
	if email == "" || !emailVerified {
		return nil, ErrEmailNotAllowed
	}

	// An account already carrying the asserted address is never adopted by the
	// sign-in. The address is what the provider asserts, and it is not proof that
	// the person is that account's owner; a provider that lets someone choose a
	// verified address would otherwise hand them the account. Attaching this
	// subject to the existing account is an explicit act the signed-in owner takes
	// in the Security Center.
	var existingID uuid.UUID
	err = s.db.Q(ctx).QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&existingID)
	switch {
	case err == nil:
		return nil, ErrAccountExists
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("look up an account by email: %w", err)
	}

	if !provider.JITProvisioning || provider.OrganizationID == nil {
		return nil, ErrNoAccount
	}

	identity, err := s.provision(ctx, provider, subject, email, displayName)
	if err != nil {
		return nil, err
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		Type: "auth.sso_provisioned", Severity: "info",
		Message:  "an account was provisioned on first SSO sign-in",
		Metadata: map[string]any{"provider": provider.Key, "email": email},
		IP:       ip, UserAgent: userAgent,
	})
	return identity, nil
}

// bindIdentity attaches a verified subject to the account that requested the
// binding. The account is re-read here, so a user deleted between the request and
// the callback cannot be linked. A subject already linked elsewhere, or an account
// already linked at this provider, is refused: two links for one pair would make
// "which account is this identity" ambiguous.
func (s *Service) bindIdentity(ctx context.Context, provider *Provider, userID uuid.UUID,
	subject, email, displayName, ip, userAgent string) (*Identity, error) {

	var (
		status string
		orgID  *uuid.UUID
	)
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoAccount
	}
	if err != nil {
		return nil, fmt.Errorf("read the account being linked: %w", err)
	}
	if status == "blocked" || status == "suspended" {
		return nil, fmt.Errorf("%w: account is not active", ErrNoAccount)
	}

	// Another account already owns this subject.
	var owner uuid.UUID
	err = s.db.Q(ctx).QueryRow(ctx,
		`SELECT user_id FROM user_identities WHERE provider_id = $1 AND subject = $2`,
		provider.ID, subject).Scan(&owner)
	switch {
	case err == nil && owner == userID:
		// Already linked to this very account: idempotent success.
		if err := s.db.Q(ctx).QueryRow(ctx,
			`SELECT organization_id FROM memberships WHERE user_id = $1 AND status = 'active'
			 ORDER BY is_org_owner DESC, created_at LIMIT 1`, userID).Scan(&orgID); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("resolve the account organization: %w", err)
			}
		}
		return &Identity{
			UserID: userID, OrganizationID: orgID, Email: email, DisplayName: displayName,
			ProviderID: provider.ID, ProviderKey: provider.Key, Bound: true,
		}, nil
	case err == nil:
		return nil, ErrIdentityAlreadyLinked
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("look up the identity being linked: %w", err)
	}

	// This account already has a different subject at this provider.
	var mine uuid.UUID
	err = s.db.Q(ctx).QueryRow(ctx,
		`SELECT subject FROM user_identities WHERE provider_id = $1 AND user_id = $2`,
		provider.ID, userID).Scan(&mine)
	if err == nil {
		return nil, ErrIdentityInUse
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("look up the account's existing identity: %w", err)
	}

	var identityID uuid.UUID
	err = s.db.Q(ctx).QueryRow(ctx,
		`INSERT INTO user_identities (user_id, provider_id, subject, email, last_login_at)
		 VALUES ($1,$2,$3,NULLIF($4,''), now())
		 RETURNING id`, userID, provider.ID, subject, email).Scan(&identityID)
	if err != nil {
		// A concurrent request can win the race between the checks above and this
		// insert. Each unique index names a different conflict, so the message says
		// which one lost rather than guessing.
		if name := uniqueViolationConstraint(err); name != "" {
			if name == "user_identities_provider_user_key" {
				return nil, ErrIdentityInUse
			}
			return nil, ErrIdentityAlreadyLinked
		}
		return nil, fmt.Errorf("link the identity to the account: %w", err)
	}

	if err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT organization_id FROM memberships WHERE user_id = $1 AND status = 'active'
		 ORDER BY is_org_owner DESC, created_at LIMIT 1`, userID).Scan(&orgID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("resolve the account organization: %w", err)
		}
	}

	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.sso_identity_linked", Severity: "info",
		Message:  "an external identity was linked to an account",
		Metadata: map[string]any{"provider": provider.Key, "subject": subject},
		IP:       ip, UserAgent: userAgent,
	})

	return &Identity{
		UserID: userID, OrganizationID: orgID, Email: email, DisplayName: displayName,
		ProviderID: provider.ID, ProviderKey: provider.Key, Bound: true, LinkIdentityID: identityID,
	}, nil
}

// UnlinkIdentity removes one of the caller's own provider links. The user id is
// part of the predicate, so a caller can only remove a link they own.
func (s *Service) UnlinkIdentity(ctx context.Context, userID, identityID uuid.UUID, ip, userAgent string) error {
	tag, err := s.db.Q(ctx).Exec(ctx,
		`DELETE FROM user_identities WHERE id = $1 AND user_id = $2`, identityID, userID)
	if err != nil {
		return fmt.Errorf("remove the identity link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrIdentityNotFound
	}
	s.audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(userID), Type: "auth.sso_identity_unlinked", Severity: "info",
		Message:  "an external identity was removed from an account",
		Metadata: map[string]any{"identity_id": identityID.String()},
		IP:       ip, UserAgent: userAgent,
	})
	return nil
}

// uniqueViolationConstraint returns the name of the unique constraint a
// PostgreSQL unique-violation error names, or "" when err is not one.
func uniqueViolationConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

// provision creates the user, its membership and its identity link in one
// transaction, so a partial account can never be left behind.
func (s *Service) provision(ctx context.Context, provider *Provider, subject, email, displayName string) (*Identity, error) {
	var (
		userID uuid.UUID
		orgID  = *provider.OrganizationID
	)
	err := s.db.WithTx(ctx, func(ctx context.Context, tx database.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, display_name, status)
			VALUES ($1, $2, 'active')
			RETURNING id`, email, displayName).Scan(&userID); err != nil {
			// A concurrent sign-in can create the same address between the check in
			// resolveIdentity and this insert. Translate the race into the same
			// domain error, not an opaque constraint failure.
			if uniqueViolationConstraint(err) != "" {
				return ErrAccountExists
			}
			return fmt.Errorf("create the account: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO memberships (user_id, organization_id, is_org_owner, status)
			VALUES ($1, $2, false, 'active')`, userID, orgID); err != nil {
			return fmt.Errorf("add the account to the organization: %w", err)
		}
		if provider.DefaultRoleKey != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_roles (user_id, role_id, organization_id)
				SELECT $1, r.id, $3 FROM roles r
				WHERE r.key = $2 AND (r.organization_id IS NULL OR r.organization_id = $3)
				ON CONFLICT DO NOTHING`, userID, provider.DefaultRoleKey, orgID); err != nil {
				return fmt.Errorf("grant the default role: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_identities (user_id, provider_id, subject, email, last_login_at)
			VALUES ($1,$2,$3,$4, now())`, userID, provider.ID, subject, email); err != nil {
			// The subject was linked to another account between the lookups and this
			// insert. Report it as a binding conflict so the caller sees one meaning.
			if uniqueViolationConstraint(err) != "" {
				return ErrIdentityAlreadyLinked
			}
			return fmt.Errorf("link the identity: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Identity{
		UserID: userID, OrganizationID: &orgID, Email: email, DisplayName: displayName,
		ProviderID: provider.ID, ProviderKey: provider.Key, Provisioned: true,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// RedirectURL is the callback registered with every provider.
func (s *Service) RedirectURL() string {
	if s.cfg.OIDC.RedirectURL != "" {
		return s.cfg.OIDC.RedirectURL
	}
	return strings.TrimRight(s.cfg.BaseURL, "/") + "/api/v1/auth/oidc/callback"
}

// LoginURL builds the route a caller uses to start a sign-in.
func (s *Service) LoginURL(providerKey, returnTo string) string {
	v := url.Values{}
	v.Set("provider", providerKey)
	if returnTo != "" {
		v.Set("return_to", returnTo)
	}
	return "/api/v1/auth/oidc/start?" + v.Encode()
}

// SafeReturnTo keeps a relative in-app path and discards anything a browser could
// resolve off-origin. It is exported because the callback handler re-validates the
// value on the way out with the same rule: two copies of this check are free to
// drift, and the drift would be an open redirect.
//
// It is a conservative allow-list rather than a list of known-bad shapes, because
// the set of strings a browser treats as off-origin is larger than it looks. The
// leading "/" is necessary but not sufficient: a browser normalises "\" to "/", so
// "/\evil.example.com" resolves to "//evil.example.com" — a protocol-relative URL
// on another host. Control bytes and spaces are refused for the same reason a
// header injection is refused: the value ends up in a Location header.
//
// Only a rooted path of unreserved characters, sub-delimiters and ":/@", with
// percent-encoded octets for anything else, is kept. An optional query and
// fragment may follow the path. Everything else becomes "/".
func SafeReturnTo(raw string) string {
	const fallback = "/"
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '/' {
		return fallback
	}
	// Split the path from an optional query and/or fragment at the first marker.
	path, suffix := raw, ""
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		path, suffix = raw[:i], raw[i:]
	}
	if path != "/" && !isSafeReturnPath(path) {
		return fallback
	}
	if !isSafeReturnSuffix(suffix) {
		return fallback
	}
	return raw
}

// isSafeReturnPath reports whether p is a rooted path that a browser cannot
// reinterpret as an authority. A second leading "/" or "\" is refused because
// either makes the reference protocol-relative.
func isSafeReturnPath(p string) bool {
	if len(p) < 2 || p[0] != '/' {
		return false
	}
	if p[1] == '/' || p[1] == '\\' {
		return false
	}
	for i := 0; i < len(p); i++ {
		if !isSafeReturnByte(p[i]) {
			return false
		}
	}
	return true
}

// isSafeReturnSuffix reports whether the query or fragment part (including its
// leading "?" or "#") is safe to place in a Location header. It is a path-safe
// byte set plus the two markers, which also covers a "#" inside a query.
func isSafeReturnSuffix(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '?', '#':
			continue
		}
		if !isSafeReturnByte(s[i]) {
			return false
		}
	}
	return true
}

// isSafeReturnByte reports whether b may appear literally in a return path.
// Unreserved characters (RFC 3986 §2.3), sub-delimiters (§2.2) plus ":", "@" and
// "/", and percent-encoded octets are allowed. Every other byte — notably "\",
// space, DEL, control bytes and raw non-ASCII — is refused, so a percent-encoded
// value is the only way to express anything unusual.
func isSafeReturnByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '.' || b == '_' || b == '~': // unreserved
		return true
	case b == '!' || b == '$' || b == '&' || b == '\'' || b == '(' || b == ')' ||
		b == '*' || b == '+' || b == ',' || b == ';' || b == '=': // sub-delims
		return true
	case b == ':' || b == '@' || b == '/':
		return true
	case b == '%': // percent-encoded octet
		return true
	}
	return false
}

func domainAllowed(email string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range domains {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate a random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) logProviderFailure(p *Provider, stage string, err error) {
	// The issuer is configuration and safe to log; the secret never appears in an
	// error from the HTTP client.
	if s.log != nil {
		s.log.Warn("identity provider call failed",
			"provider", p.Key, "issuer", p.Issuer, "stage", stage, "error", err.Error())
	}
}
