//go:build integration

// Integration tests for the MCP client credential store. They run against a real
// PostgreSQL server, because the properties that matter — a unique token hash, the
// organization filter, a revoked row being invisible — are enforced by the schema
// and the query, not by a mock.
package testsupport

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/mcpcred"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// credFixture is a tenant with one administrator who holds a broad scope.
type credFixture struct {
	db    *database.DB
	org   uuid.UUID
	other uuid.UUID
	user  uuid.UUID
	svc   *mcpcred.Service
	admin *rbac.Principal
}

func newCredFixture(t *testing.T) *credFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "mcp-org")
	other := insertOrganization(t, db, "mcp-other")
	user := seedMember(t, db, org, "tenant_admin")
	svc := mcpcred.NewService(db, audit.New(db))

	// The admin holds the manage and read permissions plus the CRM reads a token
	// might narrow into. Building the principal explicitly keeps the test from
	// depending on the seeded role set, which is checked elsewhere.
	admin := rbac.NewPrincipal(user, "admin@example.test", "Admin", org, []rbac.Permission{
		rbac.PermMCPCredentialsRead, rbac.PermMCPCredentialsManage,
		rbac.PermAccountsRead, rbac.PermTasksRead, rbac.PermTasksCreate,
	})
	return &credFixture{db: db, org: org, other: other, user: user, svc: svc, admin: admin}
}

func TestMCPCredentialCreateAndAuthenticate(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name:        "CI agent",
		Permissions: []string{"accounts.read", "tasks.read"},
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	if created.Token == "" {
		t.Fatal("create returned an empty token")
	}
	if created.Credential.Status != "active" {
		t.Fatalf("status = %q, want active", created.Credential.Status)
	}
	// The plaintext must not be recoverable from what is stored.
	if created.Credential.TokenPrefix == created.Token {
		t.Fatal("the stored prefix equals the full token")
	}

	// The token authenticates to a principal carrying exactly the granted scope,
	// never the owner's roles.
	p, cred, err := f.svc.Authenticate(ctx, created.Token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if cred.ID != created.Credential.ID {
		t.Fatalf("authenticated credential = %s, want %s", cred.ID, created.Credential.ID)
	}
	if p.UserID != f.user || p.OrganizationID != f.org {
		t.Fatalf("principal identity = %s/%s, want %s/%s", p.UserID, p.OrganizationID, f.user, f.org)
	}
	if !p.Has(rbac.PermAccountsRead) || !p.Has(rbac.PermTasksRead) {
		t.Fatal("principal lacks a granted permission")
	}
	// The admin's manage permission was NOT granted to the credential, so the token
	// must not carry it. This is the escalation guard.
	if p.Has(rbac.PermMCPCredentialsManage) || p.Has(rbac.PermTasksCreate) {
		t.Fatal("credential carries a permission it was not granted")
	}
	// A credential never carries the platform-owner override.
	if p.IsPlatformOwner {
		t.Fatal("credential principal is marked platform owner")
	}

	// The usage stamp is recorded on a successful authentication.
	if cred.LastUsedAt == nil {
		t.Fatal("last_used_at was not recorded")
	}
}

func TestMCPCredentialScopeIsNarrowedToTheOwner(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	// The admin does not hold reports.read; a credential may not grant it.
	_, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name:        "too broad",
		Permissions: []string{"reports.read"},
	})
	if !errors.Is(err, mcpcred.ErrTooBroad) {
		t.Fatalf("create with an ungranted permission: err = %v, want ErrTooBroad", err)
	}

	// An unknown permission is refused for a different reason.
	_, err = f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name:        "unknown",
		Permissions: []string{"not.a.permission"},
	})
	if !errors.Is(err, mcpcred.ErrUnknownScope) {
		t.Fatalf("create with an unknown permission: err = %v, want ErrUnknownScope", err)
	}

	// An empty scope is refused: a credential that can do nothing is a mistake.
	_, err = f.svc.Create(ctx, f.admin, mcpcred.CreateInput{Name: "empty"})
	if !errors.Is(err, mcpcred.ErrEmptyScope) {
		t.Fatalf("create with no scope: err = %v, want ErrEmptyScope", err)
	}
}

func TestMCPCredentialRevokeStopsAuthentication(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "revocable", Permissions: []string{"accounts.read"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, created.Token); err != nil {
		t.Fatalf("authenticate before revoke: %v", err)
	}

	if err := f.svc.Revoke(ctx, f.admin, created.Credential.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, created.Token); !errors.Is(err, mcpcred.ErrInvalidToken) {
		t.Fatalf("authenticate after revoke: err = %v, want ErrInvalidToken", err)
	}

	// Revoking again finds no live row and reports not-found rather than succeeding
	// silently.
	if err := f.svc.Revoke(ctx, f.admin, created.Credential.ID); !errors.Is(err, mcpcred.ErrNotFound) {
		t.Fatalf("second revoke: err = %v, want ErrNotFound", err)
	}

	// The row is kept, with a status of revoked, for the audit trail.
	list, err := f.svc.List(ctx, f.admin)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Status != "revoked" {
		t.Fatalf("list = %+v, want one revoked credential", list)
	}
}

func TestMCPCredentialRotateReplacesTheToken(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "rotatable", Permissions: []string{"accounts.read"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rotated, err := f.svc.Rotate(ctx, f.admin, created.Credential.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Token == created.Token {
		t.Fatal("rotation returned the same token")
	}
	// The new token works and the old one does not.
	if _, _, err := f.svc.Authenticate(ctx, rotated.Token); err != nil {
		t.Fatalf("authenticate with the new token: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, created.Token); !errors.Is(err, mcpcred.ErrInvalidToken) {
		t.Fatalf("authenticate with the old token: err = %v, want ErrInvalidToken", err)
	}
}

func TestMCPCredentialExpiryIsEnforced(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "short-lived", Permissions: []string{"accounts.read"}, ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Credential.ExpiresAt == nil {
		t.Fatal("expires_at was not set")
	}

	// Move the deadline into the past and confirm the token stops working and the
	// list reports it as expired.
	if _, err := f.db.Pool().Exec(ctx,
		`UPDATE mcp_credentials SET expires_at = now() - interval '1 hour' WHERE id = $1`,
		created.Credential.ID); err != nil {
		t.Fatalf("expire the credential: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, created.Token); !errors.Is(err, mcpcred.ErrInvalidToken) {
		t.Fatalf("authenticate an expired token: err = %v, want ErrInvalidToken", err)
	}
	list, err := f.svc.List(ctx, f.admin)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Status != "expired" {
		t.Fatalf("list = %+v, want one expired credential", list)
	}
}

func TestMCPCredentialIsOrganizationScoped(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "org-a", Permissions: []string{"accounts.read"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// An admin of a different organization lists nothing, and cannot revoke or
	// rotate a credential they cannot see.
	outsider := rbac.NewPrincipal(uuid.New(), "outsider@example.test", "Outsider", f.other,
		[]rbac.Permission{rbac.PermMCPCredentialsRead, rbac.PermMCPCredentialsManage})
	list, err := f.svc.List(ctx, outsider)
	if err != nil {
		t.Fatalf("list as outsider: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("outsider sees %d credentials, want 0", len(list))
	}
	if err := f.svc.Revoke(ctx, outsider, created.Credential.ID); !errors.Is(err, mcpcred.ErrNotFound) {
		t.Fatalf("outsider revoke: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Rotate(ctx, outsider, created.Credential.ID); !errors.Is(err, mcpcred.ErrNotFound) {
		t.Fatalf("outsider rotate: err = %v, want ErrNotFound", err)
	}

	// The credential is still usable after the outsider's failed attempts.
	if _, _, err := f.svc.Authenticate(ctx, created.Token); err != nil {
		t.Fatalf("authenticate after outsider attempts: %v", err)
	}
}

func TestMCPCredentialBlockedOwnerLosesAccess(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "owner-blocked", Permissions: []string{"accounts.read"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.db.Pool().Exec(ctx,
		`UPDATE users SET status = 'blocked' WHERE id = $1`, f.user); err != nil {
		t.Fatalf("block the owner: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, created.Token); !errors.Is(err, mcpcred.ErrInvalidToken) {
		t.Fatalf("authenticate with a blocked owner: err = %v, want ErrInvalidToken", err)
	}
}

func TestMCPCredentialAuditTrailOmitsTheToken(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "audited", Permissions: []string{"accounts.read"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var after string
	if err := f.db.Pool().QueryRow(ctx, `
SELECT after::text FROM audit_log
WHERE action = 'mcp.credentials.created' AND entity_id = $1`,
		created.Credential.ID.String()).Scan(&after); err != nil {
		t.Fatalf("read audit entry: %v", err)
	}
	if after == "" {
		t.Fatal("no audit entry recorded for the credential creation")
	}
	// The token, or any long substring of it, must not appear in the audit record.
	if len(created.Token) > 30 && strings.Contains(after, created.Token[10:30]) {
		t.Fatal("the audit entry leaks the token")
	}
	if strings.Contains(after, created.Token) {
		t.Fatal("the audit entry contains the full token")
	}
}

func TestMCPCredentialUniqueTokenHashIsEnforced(t *testing.T) {
	f := newCredFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.admin, mcpcred.CreateInput{
		Name: "first", Permissions: []string{"accounts.read"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Inserting the same hash again must be refused by the unique constraint, which
	// is what makes a token collision impossible rather than merely unlikely.
	hash := mcpcred.HexHash(created.Token)
	if _, err := f.db.Pool().Exec(ctx, `
INSERT INTO mcp_credentials (organization_id, owner_user_id, name, token_prefix, token_hash, permissions)
VALUES ($1, $2, 'dup', 'fcmcp_dup', decode($3, 'hex'), ARRAY['accounts.read'])`,
		f.org, f.user, hash); err == nil {
		t.Fatal("a duplicate token hash was accepted")
	} else if !database.IsUniqueViolation(err) {
		t.Fatalf("duplicate hash error = %v, want a unique violation", err)
	}
}
