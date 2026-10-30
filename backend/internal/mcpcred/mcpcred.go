// Package mcpcred implements scoped, revocable credentials for external MCP
// clients.
//
// The MCP protocol endpoint used to authenticate with the browser session cookie,
// which no long-running client can hold and no operator can revoke without ending
// the person's own login. A credential is the answer: a personal access token bound
// to one organization and one owner, carrying an explicit permission list.
//
// The list is the security property. A credential is authorized against its own
// permissions, never the owner's roles, so a read-only credential cannot mutate CRM
// data even when its owner is a platform owner. Authentication resolves the owner
// to confirm the account is still active and to inherit the owner's team membership
// for record-level rules, but the authority it carries is only what was granted.
//
// Only the SHA-256 of the token is stored; the plaintext is shown once. The first
// characters are kept as a non-secret prefix so a token can be told apart in a list.
package mcpcred

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// TokenPrefix is the literal prefix of every MCP credential token. It makes a
// leaked token recognizable — in a log, a paste, a support ticket — and is not
// secret.
const TokenPrefix = "fcmcp_"

// prefixDisplayLen is how many characters of the token are kept as the display
// prefix. It is enough to distinguish two tokens and far too little to guess one.
const prefixDisplayLen = len(TokenPrefix) + 8

// Errors returned by the service. They are sentinel values so the API can map them
// onto status codes without matching strings.
var (
	// ErrNotFound is returned when a credential does not exist in the caller's scope.
	ErrNotFound = errors.New("MCP credential not found")
	// ErrInvalidToken is returned when a presented token matches no live credential.
	ErrInvalidToken = errors.New("the MCP credential is invalid, revoked or expired")
	// ErrEmptyScope is returned when a credential would carry no permissions.
	ErrEmptyScope = errors.New("an MCP credential must carry at least one permission")
	// ErrUnknownScope is returned when a scope names a permission not in the catalog.
	ErrUnknownScope = errors.New("the MCP credential names an unknown permission")
	// ErrTooBroad is returned when a scope names a permission the owner does not hold.
	ErrTooBroad = errors.New("the MCP credential may not grant a permission its owner lacks")
)

// Credential is a stored MCP credential, without the secret.
type Credential struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	OwnerUserID    uuid.UUID  `json:"owner_user_id"`
	Name           string     `json:"name"`
	TokenPrefix    string     `json:"token_prefix"`
	Permissions    []string   `json:"permissions"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// Status is derived for the list view: active, revoked or expired. It is not
	// stored, because "expired" is a function of the clock and a stored copy would
	// go stale.
	Status string `json:"status"`
}

// CreateInput is the body for minting a credential.
type CreateInput struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	// ExpiresInHours is optional. Zero or absent means the credential does not
	// expire. It is expressed as a duration in hours rather than a timestamp so the
	// server, not the client's clock, decides the deadline.
	ExpiresInHours int `json:"expires_in_hours,omitempty"`
}

// Created is the one-time result of minting a credential. The Token field is the
// only place the plaintext ever appears; it is not stored and cannot be recovered.
type Created struct {
	Credential Credential `json:"credential"`
	Token      string     `json:"token"`
}

// Service provides credential lifecycle operations.
type Service struct {
	db    *database.DB
	audit *audit.Logger
}

// NewService builds the credential service.
func NewService(db *database.DB, auditLog *audit.Logger) *Service {
	return &Service{db: db, audit: auditLog}
}

const credentialColumns = `id, organization_id, owner_user_id, name, token_prefix,
	permissions, expires_at, revoked_at, last_used_at, created_at, updated_at`

// validateScope checks that every requested permission exists in the catalog, that
// the set is not empty, and that it is a subset of what the caller holds.
//
// The subset rule is what stops a credential from becoming a privilege-escalation
// path: an administrator may narrow their own authority into a token, never widen
// it. A platform owner holds every permission, so the check is naturally permissive
// for them.
func validateScope(p *rbac.Principal, perms []string) ([]string, error) {
	if len(perms) == 0 {
		return nil, ErrEmptyScope
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(perms))
	for _, raw := range perms {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if !rbac.Known(rbac.Permission(key)) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownScope, key)
		}
		if !p.Has(rbac.Permission(key)) {
			return nil, fmt.Errorf("%w: %q", ErrTooBroad, key)
		}
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil, ErrEmptyScope
	}
	sort.Strings(out)
	return out, nil
}

// Create mints a credential and returns it with the plaintext token, exactly once.
func (s *Service) Create(ctx context.Context, p *rbac.Principal, in CreateInput) (*Created, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return nil, &ValidationError{Fields: map[string]string{
			"name": "is required and must be at most 100 characters",
		}}
	}
	scope, err := validateScope(p, in.Permissions)
	if err != nil {
		return nil, err
	}
	if in.ExpiresInHours < 0 {
		return nil, &ValidationError{Fields: map[string]string{
			"expires_in_hours": "must not be negative",
		}}
	}

	token, hash, prefix, err := newToken()
	if err != nil {
		return nil, err
	}

	var expires *time.Time
	if in.ExpiresInHours > 0 {
		t := time.Now().Add(time.Duration(in.ExpiresInHours) * time.Hour)
		expires = &t
	}

	var cred Credential
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO mcp_credentials
				(organization_id, owner_user_id, name, token_prefix, token_hash, permissions, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING ` + credentialColumns
		scanned, err := scanCredential(s.db.Q(ctx).QueryRow(ctx, q,
			p.OrganizationID, p.UserID, name, prefix, hash, scope, expires))
		if err != nil {
			return err
		}
		cred = *scanned
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionMCPCredentialCreated),
			EntityType:     "mcp_credential",
			EntityID:       cred.ID.String(),
			// The token is never recorded. The scope is, because "what did this grant"
			// is the question an audit has to answer.
			After: map[string]any{
				"name": cred.Name, "token_prefix": cred.TokenPrefix,
				"permissions": scope, "expires_at": expires,
			},
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create MCP credential: %w", err)
	}
	cred.Status = statusOf(&cred, time.Now())
	return &Created{Credential: cred, Token: token}, nil
}

// List returns the credentials in the caller's organization, newest first. It is
// organization-scoped: one tenant never sees another's credentials.
func (s *Service) List(ctx context.Context, p *rbac.Principal) ([]Credential, error) {
	const q = `
		SELECT ` + credentialColumns + `
		FROM mcp_credentials
		WHERE organization_id = $1
		ORDER BY created_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list MCP credentials: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	out := make([]Credential, 0)
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		c.Status = statusOf(c, now)
		out = append(out, *c)
	}
	return out, rows.Err()
}

// Revoke soft-deletes a credential. A revoked credential is kept for the audit
// trail, and the lookup filters it out.
func (s *Service) Revoke(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE mcp_credentials SET revoked_at = now()
			WHERE id = $1 AND organization_id = $2 AND revoked_at IS NULL
			RETURNING id, name, token_prefix`
		var gotID uuid.UUID
		var name, prefix string
		err := s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID).Scan(&gotID, &name, &prefix)
		if err != nil {
			if database.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionMCPCredentialRevoked),
			EntityType:     "mcp_credential",
			EntityID:       gotID.String(),
			After:          map[string]any{"name": name, "token_prefix": prefix},
		})
	})
}

// Rotate issues a new token for an existing credential, keeping its name and scope
// and clearing its revocation, so a client can be handed a fresh secret without an
// operator rebuilding the credential by hand. The old token stops working the
// instant the hash is replaced, in the same transaction.
func (s *Service) Rotate(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Created, error) {
	token, hash, prefix, err := newToken()
	if err != nil {
		return nil, err
	}
	var cred Credential
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE mcp_credentials
			SET token_hash = $3, token_prefix = $4, revoked_at = NULL
			WHERE id = $1 AND organization_id = $2
			RETURNING ` + credentialColumns
		scanned, err := scanCredential(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, hash, prefix))
		if err != nil {
			if database.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		cred = *scanned
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionMCPCredentialRotated),
			EntityType:     "mcp_credential",
			EntityID:       cred.ID.String(),
			After:          map[string]any{"name": cred.Name, "token_prefix": cred.TokenPrefix},
		})
	})
	if err != nil {
		return nil, err
	}
	cred.Status = statusOf(&cred, time.Now())
	return &Created{Credential: cred, Token: token}, nil
}

// Authenticate resolves a presented token to a principal.
//
// It returns the credential's own permission set, never the owner's roles, so the
// authority of the resulting principal is exactly what was granted. The owner is
// loaded to confirm the account is still active (a blocked owner stops every token
// immediately) and to inherit team membership for record-level rules. The last-used
// timestamp is best-effort: a failure to record it must not fail an otherwise valid
// call.
func (s *Service) Authenticate(ctx context.Context, token string) (*rbac.Principal, *Credential, error) {
	if token == "" || !strings.HasPrefix(token, TokenPrefix) {
		return nil, nil, ErrInvalidToken
	}
	hash := hashToken(token)

	const q = `
		SELECT ` + credentialColumns + `
		FROM mcp_credentials
		WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`
	cred, err := scanCredential(s.db.Q(ctx).QueryRow(ctx, q, hash))
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil, ErrInvalidToken
		}
		return nil, nil, fmt.Errorf("resolve MCP credential: %w", err)
	}
	cred.Status = statusOf(cred, time.Now())

	// Confirm the owner is still active and capture their team membership. The
	// authority granted is the credential's scope, not the owner's roles.
	principal, err := s.loadOwner(ctx, cred)
	if err != nil {
		return nil, nil, err
	}

	// Best-effort usage stamp. A failure here does not deny a valid call, and the
	// returned credential reflects the stamp so the caller sees the state it just
	// wrote rather than the stale row it read.
	usedAt := time.Now()
	if _, err := s.db.Q(ctx).Exec(ctx,
		`UPDATE mcp_credentials SET last_used_at = $2 WHERE id = $1`, cred.ID, usedAt); err == nil {
		cred.LastUsedAt = &usedAt
	}

	return principal, cred, nil
}

// loadOwner builds the principal a credential acts as: the owner's identity and
// organization, but only the credential's permission set.
func (s *Service) loadOwner(ctx context.Context, cred *Credential) (*rbac.Principal, error) {
	const q = `
		SELECT u.id, u.email, u.display_name, u.status, u.is_platform_owner
		FROM users u WHERE u.id = $1`
	var (
		id          uuid.UUID
		email       string
		displayName string
		status      string
		isOwner     bool
	)
	err := s.db.Q(ctx).QueryRow(ctx, q, cred.OwnerUserID).Scan(&id, &email, &displayName, &status, &isOwner)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrInvalidToken
		}
		return nil, fmt.Errorf("load MCP credential owner: %w", err)
	}
	// A blocked or suspended owner loses every token immediately, the same way a
	// blocked user loses their sessions.
	if status == "blocked" || status == "suspended" {
		return nil, ErrInvalidToken
	}

	// The membership must still exist and be active: an owner removed from the
	// organization cannot keep using a token scoped to it.
	const memberQ = `
		SELECT count(*) FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND m.organization_id = $2
		  AND m.status = 'active' AND o.status = 'active'`
	var memberCount int
	if err := s.db.Q(ctx).QueryRow(ctx, memberQ, id, cred.OrganizationID).Scan(&memberCount); err != nil {
		return nil, fmt.Errorf("check MCP credential membership: %w", err)
	}
	if memberCount == 0 {
		return nil, ErrInvalidToken
	}

	perms := make([]rbac.Permission, 0, len(cred.Permissions))
	for _, key := range cred.Permissions {
		perms = append(perms, rbac.Permission(key))
	}

	// The credential acts with its own scope, so the principal is built without the
	// platform-owner override: a token is never wider than the list it was minted
	// with, even for an owner's account.
	p := rbac.NewPrincipal(id, email, displayName, cred.OrganizationID, perms)
	p.IsPlatformOwner = false
	p.TeamIDs = s.ownerTeams(ctx, id, cred.OrganizationID)
	return p, nil
}

// ownerTeams returns the owner's active team ids in the organization, so
// record-level "owner or team" rules behave the same for a token as for a session.
func (s *Service) ownerTeams(ctx context.Context, userID, orgID uuid.UUID) []uuid.UUID {
	const q = `
		SELECT tm.team_id FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.user_id = $1 AND t.organization_id = $2 AND t.archived_at IS NULL`
	rows, err := s.db.Q(ctx).Query(ctx, q, userID, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil
		}
		out = append(out, id)
	}
	return out
}

// ValidationError describes why an input was refused, per field, in the shape the
// CRM handlers use.
type ValidationError struct {
	Fields map[string]string
}

// Error implements error.
func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for k, v := range e.Fields {
		parts = append(parts, k+": "+v)
	}
	sort.Strings(parts)
	return "invalid MCP credential: " + strings.Join(parts, "; ")
}

// statusOf derives the display status from the clock.
func statusOf(c *Credential, now time.Time) string {
	switch {
	case c.RevokedAt != nil:
		return "revoked"
	case c.ExpiresAt != nil && !c.ExpiresAt.After(now):
		return "expired"
	default:
		return "active"
	}
}

// newToken mints a token, returning the plaintext, its hash and the display prefix.
func newToken() (token string, hash []byte, prefix string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, "", fmt.Errorf("generate MCP token: %w", err)
	}
	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	if len(token) < prefixDisplayLen {
		prefix = token
	} else {
		prefix = token[:prefixDisplayLen]
	}
	return token, hashToken(token), prefix, nil
}

// hashToken returns the SHA-256 of a token. Tokens are stored only as hashes.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// HexHash exposes the hex form of a token hash, for diagnostics and tests.
func HexHash(token string) string { return hex.EncodeToString(hashToken(token)) }

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCredential(row rowScanner) (*Credential, error) {
	var c Credential
	var perms []string
	if err := row.Scan(&c.ID, &c.OrganizationID, &c.OwnerUserID, &c.Name, &c.TokenPrefix,
		&perms, &c.ExpiresAt, &c.RevokedAt, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.Permissions = perms
	return &c, nil
}
