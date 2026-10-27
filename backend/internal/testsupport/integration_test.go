//go:build integration

// Integration tests exercise the real schema on a real PostgreSQL server. They
// are excluded from the default build because they need a database; run them with
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./internal/testsupport/...
//
// They deliberately do not use mocks: the point is to catch schema drift,
// constraint mistakes and query errors that a mock would happily accept.
package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// connectOrSkip returns a pool for the test database, or skips the test when no
// URL is configured so the default `go test ./...` stays green.
func connectOrSkip(t *testing.T) *database.DB {
	t.Helper()

	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping integration test")
	}

	db, err := database.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	t.Cleanup(db.Close)

	return db
}

// freshSchema drops and rebuilds the public schema, then applies every migration.
// This gives each test a known starting point instead of depending on whatever a
// previous run left behind.
func freshSchema(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := context.Background()

	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset the schema: %v", err)
	}

	if _, err := migrate.Up(os.Getenv("FORGECRM_TEST_DATABASE_URL"), slog.Default()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}

// TestMigrationsApplyAndAreIdempotent verifies that a clean database reaches the
// expected version and that a second run is a no-op. A migration that is not
// idempotent breaks every restart, so this is checked explicitly.
func TestMigrationsApplyAndAreIdempotent(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()

	var version int
	var dirty bool
	if err := db.Pool().QueryRow(ctx,
		`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if dirty {
		t.Fatal("schema_migrations is dirty after a successful migration")
	}
	if version < 5 {
		t.Fatalf("schema version = %d, want at least 5", version)
	}

	applied, err := migrate.Up(os.Getenv("FORGECRM_TEST_DATABASE_URL"), slog.Default())
	if err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}
	if applied != 0 {
		t.Fatalf("second migration run applied %d migrations, want 0", applied)
	}
}

// TestSeedDataMatchesThePermissionCatalog catches drift between the Go permission
// catalog and the SQL seed. If they disagree, a permission exists in code but no
// role can hold it, and enforcement silently denies legitimate access.
func TestSeedDataMatchesThePermissionCatalog(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()

	var seeded int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM permissions`).Scan(&seeded); err != nil {
		t.Fatalf("count permissions: %v", err)
	}
	catalog := rbac.All()
	if want := len(catalog); seeded != want {
		t.Fatalf("seeded permissions = %d, the catalog defines %d", seeded, want)
	}

	rows, err := db.Pool().Query(ctx, `SELECT key FROM permissions`)
	if err != nil {
		t.Fatalf("list permission keys: %v", err)
	}
	defer rows.Close()

	present := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan permission key: %v", err)
		}
		present[key] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate permission keys: %v", err)
	}

	for _, p := range catalog {
		if !present[string(p)] {
			t.Errorf("permission %q is in the catalog but not seeded", p)
		}
	}

	var templates int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM roles WHERE organization_id IS NULL`).Scan(&templates); err != nil {
		t.Fatalf("count platform role templates: %v", err)
	}
	if templates == 0 {
		t.Fatal("no platform role templates were seeded; new organizations cannot be provisioned")
	}
}

// TestCrossOrganizationIsolation is the core tenant-isolation check: a record in
// one organization must be invisible to a principal scoped to another.
func TestCrossOrganizationIsolation(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()

	orgA := insertOrganization(t, db, "org-a")
	orgB := insertOrganization(t, db, "org-b")

	accountA := insertAccount(t, db, orgA, "Account in A")

	var found uuid.UUID
	err := db.Pool().QueryRow(ctx,
		`SELECT id FROM accounts WHERE id = $1 AND organization_id = $2`,
		accountA, orgB).Scan(&found)
	if err == nil {
		t.Fatal("a query scoped to organization B returned a record from organization A")
	}

	if err := db.Pool().QueryRow(ctx,
		`SELECT id FROM accounts WHERE id = $1 AND organization_id = $2`,
		accountA, orgA).Scan(&found); err != nil {
		t.Fatalf("the owning organization could not read its own record: %v", err)
	}
}

// TestDatabaseConstraintsAreEnforced verifies that the schema, not only the
// application, rejects bad data. A missing constraint is a data-integrity bug
// that surfaces long after the write.
func TestDatabaseConstraintsAreEnforced(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "constraints")

	t.Run("organization slug is unique", func(t *testing.T) {
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO organizations (name, slug) VALUES ($1, $2)`, "Duplicate", "constraints")
		if err == nil {
			t.Fatal("a duplicate organization slug was accepted")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("expected a unique violation (23505), got %v", err)
		}
	})

	t.Run("a foreign key cannot be bypassed", func(t *testing.T) {
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO contacts (organization_id, account_id, first_name, last_name)
			 VALUES ($1, $2, $3, $4)`,
			org, uuid.New(), "Ghost", "Reference")
		if err == nil {
			t.Fatal("a contact referencing a missing account was accepted")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("expected a foreign key violation (23503), got %v", err)
		}
	})
}

// TestTransactionRollbackLeavesNoTrace checks that a failed transaction does not
// partially commit. The CRM layer relies on this for multi-statement writes.
func TestTransactionRollbackLeavesNoTrace(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "rollback")

	sentinel := errors.New("deliberate failure")

	err := db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if _, err := db.Q(ctx).Exec(ctx,
			`INSERT INTO accounts (organization_id, name) VALUES ($1, $2)`,
			org, "Should not survive"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx returned %v, want the sentinel error", err)
	}

	var count int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE organization_id = $1`, org).Scan(&count); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("a rolled-back transaction left %d rows behind", count)
	}
}

// TestOutboxDrainsWithoutNullScanError reproduces the defect where FetchPending
// scanned a nullable correlation_id into a non-pointer string, which made every
// poll fail. The event carries no correlation identifier, which is the case that
// used to break.
func TestOutboxDrainsWithoutNullScanError(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "outbox")

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1)`, org)
	if err != nil {
		t.Fatalf("insert an outbox event: %v", err)
	}

	rows, err := db.Pool().Query(ctx, `
		SELECT id, event_type, correlation_id
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY occurred_at
		LIMIT 10`)
	if err != nil {
		t.Fatalf("query the outbox: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var (
			id            uuid.UUID
			eventType     string
			correlationID *string
		)
		if err := rows.Scan(&id, &eventType, &correlationID); err != nil {
			t.Fatalf("scanning an event with a null correlation_id failed: %v", err)
		}
		if correlationID != nil {
			t.Fatalf("correlation_id = %v, want nil", *correlationID)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the outbox: %v", err)
	}
	if seen != 1 {
		t.Fatalf("read %d outbox events, want 1", seen)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func insertOrganization(t *testing.T, db *database.DB, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id`,
		"Org "+slug, slug).Scan(&id); err != nil {
		t.Fatalf("insert organization %q: %v", slug, err)
	}
	return id
}

func insertAccount(t *testing.T, db *database.DB, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (organization_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name).Scan(&id); err != nil {
		t.Fatalf("insert account %q: %v", name, err)
	}
	return id
}
