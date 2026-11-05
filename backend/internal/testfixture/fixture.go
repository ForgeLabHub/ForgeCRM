// Package testfixture builds reproducible tenants, users and domain records for
// tests that need a real database.
//
// Every database-backed test used to assemble its own organization, user,
// membership and role rows by hand. That is a page of SQL per test, copied and
// drifted, and it hides the two facts a reader most needs: which tenant the test
// acts as, and which permissions that tenant holds. This package makes those two
// facts the arguments and everything else a detail.
//
// The package is test-only scaffolding. It lives outside internal/testsupport so
// testsupport's own suite can use it, and it is not a _test.go file so
// `go build ./...` compiles it and CI keeps it honest. Production code must not
// import it; internal/archtest enforces that.
package testfixture

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/testdb"
)

// DB returns a pool for the calling test binary's private database, reset to a
// known empty schema.
//
// The reset drops and rebuilds the public schema, then reapplies every migration.
// It is safe because testdb hands each test binary its own database, so this
// cannot disturb a package running beside it. A test that wants the data its
// predecessor left behind should call testdb.Connect instead.
func DB(t testing.TB) *database.DB {
	t.Helper()
	db := testdb.Connect(t)
	reset(t, db)
	return db
}

// reset drops and rebuilds the public schema, then reapplies every migration, so
// a test starts from a known state instead of whatever a previous test left.
func reset(t testing.TB, db *database.DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset the schema: %v", err)
	}
	if _, err := migrate.Up(testdb.URL(t), slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}

// Org is one organization with the members and roles a test needs to act as a
// tenant. The zero value is not usable; build it with Tenant.
type Org struct {
	ID       uuid.UUID
	Slug     string
	Name     string
	OwnerID  uuid.UUID
	owner    *rbac.Principal
	viewerID uuid.UUID
	viewer   *rbac.Principal
}

// Tenant provisions a fresh organization with a cloned role catalog, an owner
// and a second user that shares the organization.
//
// The slug is suffixed with a short random token so two tenants in one test, or
// a tenant left by a previous run, cannot collide on the unique slug index.
func Tenant(t testing.TB, db *database.DB, slug string) *Org {
	t.Helper()
	ctx := context.Background()

	orgID := uuid.New()
	unique := slug + "-" + shortID()

	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
		orgID, "Org "+slug, unique); err != nil {
		t.Fatalf("create organization %q: %v", slug, err)
	}

	// An organization is only usable once the platform role templates are cloned
	// into it. Without this a role assignment has nothing to point at.
	if _, err := rbac.NewService(db).ClonePlatformRoles(ctx, orgID); err != nil {
		t.Fatalf("clone role templates for %q: %v", slug, err)
	}

	o := &Org{ID: orgID, Slug: unique, Name: "Org " + slug}
	o.OwnerID, o.owner = o.addUser(t, db, "owner", "tenant_admin", true)
	o.viewerID, o.viewer = o.addUser(t, db, "viewer", "readonly_auditor", false)
	return o
}

// Owner returns a principal that holds the tenant_admin role in the organization.
func (o *Org) Owner() *rbac.Principal { return o.owner }

// Viewer returns a principal that holds a read-only role in the same
// organization. It exists so a test can assert that a permitted owner and an
// under-privileged peer are treated differently without building the second
// principal by hand.
func (o *Org) Viewer() *rbac.Principal { return o.viewer }

// User adds one more member to the organization, granted the named system role.
func (o *Org) User(t testing.TB, db *database.DB, roleKey string) *rbac.Principal {
	t.Helper()
	_, p := o.addUser(t, db, roleKey, roleKey, false)
	return p
}

// addUser creates a user, a membership and a role grant, then resolves the
// principal the way the API does: from the roles the user actually holds. A
// principal built from a hand-written permission list would not exercise the
// resolution path, which is where tenant scoping lives.
func (o *Org) addUser(t testing.TB, db *database.DB, label, roleKey string, isOwner bool) (uuid.UUID, *rbac.Principal) {
	t.Helper()
	ctx := context.Background()

	userID := uuid.New()
	email := fmt.Sprintf("%s-%s@example.test", label, shortID())
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO users (id, email, display_name, status) VALUES ($1, $2, $3, 'active')`,
		userID, email, "User "+label); err != nil {
		t.Fatalf("create user %q: %v", label, err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO memberships (user_id, organization_id, is_org_owner) VALUES ($1, $2, $3)`,
		userID, o.ID, isOwner); err != nil {
		t.Fatalf("create membership for %q: %v", label, err)
	}

	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO user_roles (user_id, organization_id, role_id)
		SELECT $1, $2, id FROM roles WHERE organization_id = $2 AND key = $3`,
		userID, o.ID, roleKey); err != nil {
		t.Fatalf("grant role %q to %q: %v", roleKey, label, err)
	}

	perms, err := rbac.NewService(db).EffectivePermissions(ctx, userID, o.ID)
	if err != nil {
		t.Fatalf("resolve permissions for %q: %v", label, err)
	}
	p := rbac.NewPrincipal(userID, email, "User "+label, o.ID, perms)
	p.IsOrgOwner = isOwner
	return userID, p
}

// shortID returns a short, unique, lowercase token safe for a slug suffix.
func shortID() string {
	return uuid.NewString()[:8]
}
