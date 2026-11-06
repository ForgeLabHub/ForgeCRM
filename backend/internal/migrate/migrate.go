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
	"sync"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

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

	// A database ahead of this build is not a failure. It happens when a deploy
	// rolls back to an earlier binary while the schema a newer build already
	// applied stays in place, and there is nothing left to apply. Asking
	// golang-migrate to move forward from a version its embedded source does not
	// contain fails with "no migration found for version N", which would take the
	// process down on every start — the outage this guard prevents. checkVersion
	// already treats applied > expected as ready, so the runner agrees with the gate.
	if expected, expErr := ExpectedVersion(); expErr == nil && before > expected {
		log.Info("database schema is ahead of this build; nothing to apply",
			"applied", before, "expected", expected)
		return 0, nil
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

// ExpectedVersion is the highest migration version compiled into this binary.
// A database below it is missing schema the running code was built against, so a
// handler added by a newer migration would fail the same way an absent table
// does.
//
// The answer is fixed for the life of the process, so it is computed once: the
// readiness probe calls this on every poll.
func ExpectedVersion() (uint, error) {
	return expectedVersionOnce()
}

var expectedVersionOnce = sync.OnceValues(func() (uint, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("load embedded migrations: %w", err)
	}
	defer func() { _ = src.Close() }()

	first, err := src.First()
	if err != nil {
		return 0, fmt.Errorf("read first migration: %w", err)
	}
	highest := first
	for cur := first; ; {
		next, err := src.Next(cur)
		if err != nil {
			break
		}
		highest = next
		cur = next
	}
	return highest, nil
})

// Queryer is the narrow slice of a connection pool this package reads through.
// It is defined here rather than importing *pgxpool so the package keeps a
// single third-party dependency (golang-migrate) and lifts out of the monolith
// on its own; *pgxpool.Pool satisfies it as-is.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Check verifies, against a live connection, that the schema is present, usable
// and current: every RequiredTable exists and the applied migration version is
// not behind this binary. It is the check the API and worker use to refuse to
// serve before the schema is ready.
//
// Checking the version as well as the tables closes a gap the table list cannot:
// a database one migration behind has every listed table, so the probe reported
// ready while a feature added by the newer migration answered 500 with
// `relation "..." does not exist`. That is the same failure the table check
// exists to prevent, just one migration later.
//
// It stays cheap — one catalog query and one version read — so the readiness
// probe can run it on every poll without measuring a table scan.
func Check(ctx context.Context, db Queryer) error {
	rows, err := db.Query(ctx,
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
	return checkVersion(ctx, db)
}

// WaitForSchema polls Check until it passes or ctx is done, and returns the last
// failure so a caller can log why it gave up.
//
// It exists for a process that must not run migrations itself — the worker — but
// must not serve before the schema is ready either. The API applies migrations;
// the worker waits for them. The loop is deliberately identical to the API's own
// startup guard, so both binaries agree on what "ready" means: the required
// tables present, the recorded version not behind, and not dirty.
//
// A caller bounds the wait with ctx: a schema that never arrives is a failed
// deploy, and the process should exit with the reason rather than hang. The
// interval is fixed rather than exponential because the wait is normally a few
// seconds while the API finishes migrating, and a slow ramp would add latency to
// every clean start to save queries on a start that is already failing.
func WaitForSchema(ctx context.Context, db Queryer, interval time.Duration, log *slog.Logger) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Keep the last schema-readiness diagnosis separate from the last raw error.
	// The final Check can race the deadline and fail with "context deadline
	// exceeded" instead of the schema state, and reporting that would hide the
	// reason the wait gave up — which is the one thing this error is for.
	var lastErr error
	for {
		err := Check(ctx, db)
		if err == nil {
			return nil
		}
		if IsSchemaNotReady(err) || lastErr == nil {
			lastErr = err
		}

		if log != nil {
			log.Info("waiting for the database schema to be ready", "reason", err.Error())
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("schema not ready after waiting: %w", lastErr)
		case <-ticker.C:
		}
	}
}

// checkVersion reads the recorded migration version once and refuses the two
// states the table check above cannot see: a schema a failed migration left
// half-applied (dirty), and a schema older than this binary.
//
// The dirty flag matters for the same reason the table list does. When a
// migration fails partway, golang-migrate records the version and sets dirty,
// but the tables already created by earlier steps exist, so the table check
// passes. Serving from that schema means the first query touching a column the
// failed migration was adding dies with `column ... does not exist` — an absent
// table one step later, and the failure this whole guard exists to prevent.
//
// A database ahead of the binary is allowed: applying migrations before rolling
// out the new code is the normal, zero-downtime order, and an older process
// reading a slightly newer schema is the safe direction.
//
// When no version is recorded — a schema assembled by another tool — the check
// stands down rather than guess, because the table check above is then the only
// reliable signal. The version table is read directly rather than through the
// migrate library so the readiness probe keeps to one catalog query and one
// cheap row read.
func checkVersion(ctx context.Context, db Queryer) error {
	var (
		applied uint
		dirty   bool
	)
	err := db.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&applied, &dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return nil
	}
	if dirty {
		return &SchemaDirtyError{Version: applied}
	}
	expected, err := ExpectedVersion()
	if err != nil {
		return nil
	}
	if applied < expected {
		return &SchemaBehindError{Applied: applied, Expected: expected}
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

// SchemaBehindError reports a schema that exists but predates this binary.
type SchemaBehindError struct {
	Applied  uint
	Expected uint
}

func (e *SchemaBehindError) Error() string {
	return fmt.Sprintf("database schema is behind this build: applied migration %d, this build expects %d; "+
		"run migrations first (FORGECRM_MIGRATE_ON_START=true or `forgecrm-api -migrate`)", e.Applied, e.Expected)
}

// SchemaDirtyError reports a schema a migration left half-applied. It is
// deliberately not recoverable by re-running migrations: golang-migrate refuses
// to continue from a dirty version, and no migration deletes data, so an
// operator has to inspect the failed step and repair it.
type SchemaDirtyError struct {
	Version uint
}

func (e *SchemaDirtyError) Error() string {
	return fmt.Sprintf("database schema is dirty at migration %d: a migration failed partway and must be "+
		"repaired before this process can serve (migrations never delete data)", e.Version)
}

// IsSchemaNotReady reports whether err is a schema-readiness failure, whether the
// schema is absent, dirty, or merely behind.
func IsSchemaNotReady(err error) bool {
	var notReady *SchemaNotReadyError
	if errors.As(err, &notReady) {
		return true
	}
	var behind *SchemaBehindError
	if errors.As(err, &behind) {
		return true
	}
	var dirty *SchemaDirtyError
	return errors.As(err, &dirty)
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
