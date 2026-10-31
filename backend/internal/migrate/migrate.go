// Package migrate runs embedded SQL migrations and reports schema readiness.
//
// Migrations are compiled into the binary so a deployed version can never drift
// from the schema it expects. The runner is idempotent: applying an already
// up-to-date database is a no-op, and golang-migrate takes a PostgreSQL advisory
// lock for the duration so two processes starting at once serialise instead of
// interleaving.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geff1778/forgecrm/internal/migrations"
)

// lockTimeout bounds how long a process waits for the migration advisory lock.
// Without it a stuck holder makes every other starter wait forever, which looks
// like a hung deploy rather than a lock conflict.
const lockTimeout = 2 * time.Minute

// State describes the schema as the database actually reports it.
type State struct {
	// Version is the applied migration version, 0 when none has been applied.
	Version uint
	// Dirty is true when a migration failed midway and the schema needs an
	// operator; the runner refuses to continue in that state.
	Dirty bool
	// Applied is true when a version table exists and holds a version.
	Applied bool
}

// Up applies all pending migrations. It returns the number of applied steps.
//
// A "dirty" database is refused rather than retried: golang-migrate marks a
// version dirty when its migration failed partway, and continuing from there
// would build on a half-applied schema. The error names the version so an
// operator can inspect and repair it, which is the only safe path. No migration
// deletes data, so a repair is never "drop and recreate".
func Up(databaseURL string, log *slog.Logger) (int, error) {
	m, err := open(databaseURL)
	if err != nil {
		return 0, err
	}
	defer closeQuietly(m, log)

	before, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("database schema is dirty at version %d; "+
			"inspect the failed migration and repair it before retrying (migrations never delete data)", before)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info("schema up to date", "version", before)
			return 0, nil
		}
		return 0, fmt.Errorf("apply migrations: %w", err)
	}

	after, _, err := m.Version()
	if err != nil {
		return 0, fmt.Errorf("read schema version after migrate: %w", err)
	}
	applied := 0
	if before == 0 {
		applied = int(after)
	} else {
		applied = int(after - before)
	}
	log.Info("migrations applied", "from", before, "to", after, "applied", applied)
	return applied, nil
}

// Down rolls back all migrations. Used only by tests and explicit operator action.
func Down(databaseURL string, log *slog.Logger) error {
	m, err := open(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	log.Info("migrations rolled back")
	return nil
}

// Status reports the schema state without changing it.
func Status(databaseURL string) (State, error) {
	m, err := open(databaseURL)
	if err != nil {
		return State{}, err
	}
	defer func() { _, _ = m.Close() }()

	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read schema version: %w", err)
	}
	return State{Version: version, Dirty: dirty, Applied: true}, nil
}

// RequiredTables are the tables an un-migrated database is missing and that the
// first authenticated request touches. They are checked by name so the readiness
// probe reports a schema problem as a schema problem, not as an opaque query
// failure on whichever handler happened to run first.
var RequiredTables = []string{"users", "organizations", "sessions", "schema_migrations"}

// Check verifies, against a live connection, that the schema is present and
// usable: every RequiredTable exists. It is the check the API and worker use to
// refuse to serve before the schema is ready.
//
// It is deliberately cheap — one catalog query — so the readiness probe can run
// it on every poll without measuring a table scan.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx,
		`SELECT c.relname
   FROM pg_class c
   JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind = 'r' AND n.nspname = current_schema()
    AND c.relname = ANY($1)`, RequiredTables)
	if err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan schema row: %w", err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}

	var missing []string
	for _, table := range RequiredTables {
		if !found[table] {
			missing = append(missing, table)
		}
	}
	if len(missing) > 0 {
		return &SchemaNotReadyError{Missing: missing}
	}
	return nil
}

// SchemaNotReadyError reports which required tables are absent.
type SchemaNotReadyError struct {
	Missing []string
}

func (e *SchemaNotReadyError) Error() string {
	return fmt.Sprintf("database schema is not ready: missing table(s) %v; "+
		"run migrations first (FORGECRM_MIGRATE_ON_START=true or `forgecrm-api -migrate`)", e.Missing)
}

// IsSchemaNotReady reports whether err is a schema-readiness failure.
func IsSchemaNotReady(err error) bool {
	var e *SchemaNotReadyError
	return errors.As(err, &e)
}

func open(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("load embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("init migrate: %w", err)
	}
	// A bounded wait turns a stuck advisory-lock holder into a visible startup
	// error instead of an apparently hung process.
	m.LockTimeout = lockTimeout
	return m, nil
}

func closeQuietly(m *migrate.Migrate, log *slog.Logger) {
	if sErr, dErr := m.Close(); sErr != nil || dErr != nil {
		log.Debug("migrate close", "source_err", sErr, "db_err", dErr)
	}
}
