//go:build integration

package testsupport

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/bootstrap"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
)

// This file covers the failure this change set is about: a database that is
// reachable but has no schema, and the guards that must refuse to serve it. The
// tests use a real PostgreSQL server because the thing under test is the
// database's own catalog — a mock would report whatever the mock was told.

// TestSchemaCheckFailsOnAnEmptyDatabase is the regression test for
// `relation "users" does not exist`. Before the guard, the API listened on an
// empty database and every query failed at request time. The check must report
// the missing tables by name so the failure is diagnosable from the log alone.
func TestSchemaCheckFailsOnAnEmptyDatabase(t *testing.T) {
	db := connectOrSkip(t)
	ctx := context.Background()

	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset the schema: %v", err)
	}

	err := migrate.Check(ctx, db.Pool())
	if err == nil {
		t.Fatal("the schema check passed on an empty database; readiness would report healthy")
	}
	if !migrate.IsSchemaNotReady(err) {
		t.Fatalf("the error is not a schema-readiness failure: %v", err)
	}
	for _, table := range []string{"users", "organizations"} {
		if !strings.Contains(err.Error(), table) {
			t.Fatalf("the error does not name the missing table %q: %v", table, err)
		}
	}
}

// TestSchemaCheckPassesAfterMigrations is the positive half: once migrations
// have run, the same check must pass, so a healthy deployment is not refused.
func TestSchemaCheckPassesAfterMigrations(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	if err := migrate.Check(context.Background(), db.Pool()); err != nil {
		t.Fatalf("the schema check failed after a successful migration: %v", err)
	}
}

// TestMigrateUpReportsVersionAndIsIdempotent checks that a fresh database reaches
// the current version and that a second run applies nothing, which is what makes
// a restart safe.
func TestMigrateUpReportsVersionAndIsIdempotent(t *testing.T) {
	db := connectOrSkip(t)
	ctx := context.Background()

	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset the schema: %v", err)
	}

	first, err := migrate.Up(os.Getenv("FORGECRM_TEST_DATABASE_URL"), slog.Default())
	if err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if first == 0 {
		t.Fatal("the first migration run applied nothing on an empty database")
	}

	second, err := migrate.Up(os.Getenv("FORGECRM_TEST_DATABASE_URL"), slog.Default())
	if err != nil {
		t.Fatalf("second migration run: %v", err)
	}
	if second != 0 {
		t.Fatalf("the second migration run applied %d migrations, want 0", second)
	}

	state, err := migrate.Status(os.Getenv("FORGECRM_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("read schema status: %v", err)
	}
	if !state.Applied || state.Dirty {
		t.Fatalf("schema status = %+v, want applied and clean", state)
	}
}

// TestBootstrapCreatesTheFirstOwnerOnce verifies the bootstrap contract: it
// creates the owner when there is none, is a no-op afterwards, and never
// overwrites. The second call deliberately supplies a different address, which
// is what an ENV value left in a deploy script would look like.
func TestBootstrapCreatesTheFirstOwnerOnce(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()
	log := slog.Default()

	first, err := bootstrap.Admin(ctx, db, log, bootstrap.Options{
		Email:    "Owner@Example.com",
		Password: "Str0ng-Bootstrap-Pass!",
	})
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if !first.Created {
		t.Fatal("the first bootstrap did not create an owner")
	}

	var count int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM users WHERE is_platform_owner`).Scan(&count); err != nil {
		t.Fatalf("count platform owners: %v", err)
	}
	if count != 1 {
		t.Fatalf("platform owners after bootstrap = %d, want 1", count)
	}

	// The stored address is normalised to lower case, so sign-in by the typed
	// address and by the stored one agree.
	var stored string
	if err := db.Pool().QueryRow(ctx,
		`SELECT email FROM users WHERE is_platform_owner`).Scan(&stored); err != nil {
		t.Fatalf("read the owner address: %v", err)
	}
	if stored != "owner@example.com" {
		t.Fatalf("stored owner address = %q, want the lower-cased form", stored)
	}

	second, err := bootstrap.Admin(ctx, db, log, bootstrap.Options{
		Email:    "attacker@example.com",
		Password: "Another-Str0ng-Pass!",
	})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if second.Created {
		t.Fatal("the second bootstrap created another owner")
	}
	if !second.AlreadyExists {
		t.Fatal("the second bootstrap did not report the existing owner")
	}

	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM users WHERE is_platform_owner`).Scan(&count); err != nil {
		t.Fatalf("count platform owners after the second run: %v", err)
	}
	if count != 1 {
		t.Fatalf("platform owners after the second run = %d, want 1 (the first owner must not be overwritten)", count)
	}
	if err := db.Pool().QueryRow(ctx,
		`SELECT email FROM users WHERE is_platform_owner`).Scan(&stored); err != nil {
		t.Fatalf("read the owner address after the second run: %v", err)
	}
	if stored != "owner@example.com" {
		t.Fatalf("the owner address changed to %q; a bootstrap value must not overwrite an existing owner", stored)
	}
}

// TestBootstrapRejectsIncompleteConfiguration checks that half a configuration is
// an error naming the missing variable, and that a password is never defaulted.
func TestBootstrapRejectsIncompleteConfiguration(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()

	_, err := bootstrap.Admin(ctx, db, slog.Default(), bootstrap.Options{Email: "owner@example.com"})
	if err == nil {
		t.Fatal("a bootstrap with an email but no password was accepted")
	}
	if !strings.Contains(err.Error(), "FORGECRM_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("the error does not name the missing variable: %v", err)
	}

	_, err = bootstrap.Admin(ctx, db, slog.Default(), bootstrap.Options{Password: "Str0ng-Pass!"})
	if err == nil {
		t.Fatal("a bootstrap with a password but no email was accepted")
	}
	if !strings.Contains(err.Error(), "FORGECRM_BOOTSTRAP_ADMIN_EMAIL") {
		t.Fatalf("the error does not name the missing variable: %v", err)
	}
}

// TestBootstrapIsANoOpWhenUnconfigured keeps the API able to call it on every
// start without bootstrap variables set in production.
func TestBootstrapIsANoOpWhenUnconfigured(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	result, err := bootstrap.Admin(context.Background(), db, slog.Default(), bootstrap.Options{})
	if err != nil {
		t.Fatalf("an unconfigured bootstrap must be a no-op, got: %v", err)
	}
	if result.Created || result.AlreadyExists {
		t.Fatalf("an unconfigured bootstrap did something: %+v", result)
	}
}

// TestConcurrentBootstrapCreatesExactlyOneOwner exercises the advisory lock: two
// processes starting at once must not both create an owner. The partial unique
// index is the backstop, but the lock is what makes the second run a clean no-op
// rather than a constraint error.
func TestConcurrentBootstrapCreatesExactlyOneOwner(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()

	const workers = 4
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := bootstrap.Admin(ctx, db, slog.Default(), bootstrap.Options{
				Email:    "owner@example.com",
				Password: "Str0ng-Bootstrap-Pass!",
			})
			errCh <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("a concurrent bootstrap failed: %v", err)
		}
	}

	var count int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM users WHERE is_platform_owner`).Scan(&count); err != nil {
		t.Fatalf("count platform owners: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent bootstrap created %d owners, want exactly 1", count)
	}
}

// TestBootstrapRequiresAMigratedSchema documents that the bootstrap reads the
// users table and therefore depends on migrations. The API checks the schema
// before calling it, so an un-migrated database fails with a schema error rather
// than an opaque "relation does not exist".
func TestBootstrapRequiresAMigratedSchema(t *testing.T) {
	db := connectOrSkip(t)
	ctx := context.Background()

	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset the schema: %v", err)
	}

	if err := migrate.Check(ctx, db.Pool()); err == nil {
		t.Fatal("the schema check passed on an empty database")
	}
}

var _ = database.DefaultMaxConns
