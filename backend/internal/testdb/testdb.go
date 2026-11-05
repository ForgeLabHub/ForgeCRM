// Package testdb provisions an isolated PostgreSQL database for each test
// binary that needs one.
//
// Integration packages used to share a single database and reset the public
// schema in setup. Two packages running at once then fought over the same
// tables — one dropped the schema another was seeding — which is why the CI
// integration step has to pass -p 1 and why a local `go test ./...` fails with
// "relation \"jobs\" does not exist". Giving each test binary its own database
// removes that constraint: the packages no longer observe each other, so they
// can run in parallel and a failure is a product failure rather than a
// scheduling accident.
//
// The package is test-only scaffolding. It lives outside internal/testsupport so
// that testsupport's own suite can use it without importing itself, and it is
// deliberately not a _test.go file so a plain `go build ./...` compiles it and
// CI keeps it honest.
package testdb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
)

// URLEnv is the variable every database-backed test reads. When it is unset the
// tests skip, so the default `go test ./...` stays green on a machine without
// PostgreSQL.
const URLEnv = "FORGECRM_TEST_DATABASE_URL"

// RedisURLEnv is the variable the Redis-backed tests read. It lives here beside
// URLEnv so the two integration prerequisites have one canonical name each, and
// the environment guard in this package can assert both without a literal that
// could drift from the one the tests read.
const RedisURLEnv = "FORGECRM_TEST_REDIS_URL"

var (
	once       sync.Once
	base       *url.URL
	label      string
	provisionE error
)

// Connect returns a pool for a database private to this test binary.
//
// The first call creates a fresh database named after the calling package,
// applies every embedded migration, and reuses it for the rest of the run. The
// caller must close the pool with Close.
func Connect(t testing.TB) *database.DB {
	t.Helper()

	raw := os.Getenv(URLEnv)
	if raw == "" {
		t.Skipf("%s is not set; skipping database-backed test", URLEnv)
	}

	once.Do(func() {
		base, provisionE = url.Parse(raw)
		if provisionE != nil {
			provisionE = fmt.Errorf("parse %s: %w", URLEnv, provisionE)
			return
		}
		label = packageLabel()
		if provisionE = createDatabase(base, label); provisionE != nil {
			return
		}
		// Migrate once, at provisioning, so a caller that only wants a pool (and
		// never resets the schema itself) still finds a usable database. Migrating
		// on every Connect would break the tests that deliberately dirty the schema
		// to exercise the readiness check.
		if _, provisionE = migrate.Up(databaseURL(base, label), discardLogger()); provisionE != nil {
			provisionE = fmt.Errorf("apply migrations to %q: %w", label, provisionE)
		}
	})
	if provisionE != nil {
		t.Fatalf("provision test database: %v", provisionE)
	}

	db, err := database.Connect(context.Background(), databaseURL(base, label))
	if err != nil {
		t.Fatalf("connect to %q: %v", label, err)
	}
	t.Cleanup(db.Close)
	return db
}

// URL returns the connection string for this test binary's private database.
// Callers that need to reset the schema themselves (migrate.Up, a DROP SCHEMA)
// use it instead of reading the environment, which still points at the shared
// database.
func URL(t testing.TB) string {
	t.Helper()
	if base == nil {
		// Connect has not run yet; prime it so base and label are populated.
		Connect(t)
	}
	return databaseURL(base, label)
}

// createDatabase drops any database left by a previous run and recreates it, so
// a crashed run does not poison the next one.
func createDatabase(base *url.URL, label string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL(base))
	if err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	defer admin.Close(ctx)

	// WITH (FORCE) terminates leftover sessions; without it a pooled connection
	// from a previous run blocks the drop indefinitely.
	quoted := pgx.Identifier{label}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop %q: %w", label, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		return fmt.Errorf("create %q: %w", label, err)
	}
	return nil
}

// adminURL points at the maintenance database on the same server, which is what
// CREATE DATABASE needs: you cannot create a database from inside the one you
// are connected to.
func adminURL(base *url.URL) string {
	u := *base
	u.Path = "/postgres"
	return u.String()
}

// databaseURL points at the provisioned database, preserving the query string
// (sslmode and friends) the operator supplied.
func databaseURL(base *url.URL, label string) string {
	u := *base
	u.Path = "/" + label
	return u.String()
}

// packageLabel derives a stable, valid database name from the calling package.
// It uses the working directory rather than reflect/runtime so it survives the
// compiled-test working-directory conventions Go already relies on, and appends
// the PID so two invocations of the same package on one machine cannot collide.
func packageLabel() string {
	dir, err := os.Getwd()
	if err != nil {
		dir = "unknown"
	}
	name := sanitize(filepath.Base(dir))
	return fmt.Sprintf("forgecrm_test_%s_%d", name, os.Getpid())
}

// sanitize reduces s to the characters PostgreSQL allows in an unquoted
// identifier. The name is still quoted at the call site; this just keeps it
// readable and short.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		out = "pkg"
	}
	return out
}

// discardLogger keeps migration output out of a test's own log assertions.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
