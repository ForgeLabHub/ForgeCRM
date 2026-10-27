// Package migrate runs embedded SQL migrations.
//
// Migrations are compiled into the binary so a deployed version can never drift
// from the schema it expects. The runner is idempotent: applying an already
// up-to-date database is a no-op.
package migrate

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/geff1778/forgecrm/internal/migrations"
)

// Up applies all pending migrations. It returns the number of applied steps.
func Up(databaseURL string, log *slog.Logger) (int, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("load embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return 0, fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		if sErr, dErr := m.Close(); sErr != nil || dErr != nil {
			log.Debug("migrate close", "source_err", sErr, "db_err", dErr)
		}
	}()

	before, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("database schema is dirty at version %d; manual intervention required", before)
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
	log.Info("migrations applied", "from", before, "to", after)
	return applied, nil
}

// Down rolls back all migrations. Used only by tests and explicit operator action.
func Down(databaseURL string, log *slog.Logger) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	log.Info("migrations rolled back")
	return nil
}
