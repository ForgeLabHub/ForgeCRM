// Package database manages the PostgreSQL connection pool and transactions.
package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgx pool with helpers used across the application.
type DB struct {
	pool *pgxpool.Pool
}

// Pool exposes the underlying pool for code that needs it directly.
func (d *DB) Pool() *pgxpool.Pool { return d.pool }

// Close releases all connections.
func (d *DB) Close() { d.pool.Close() }

// DefaultMaxConns is the per-process connection budget used when a caller does
// not choose one.
//
// The figure that matters in production is replicas x MaxConns, and it has to
// stay below the server's max_connections minus the superuser reserve. Four API
// replicas and one worker at 16 each is 80 connections, which fits PostgreSQL's
// default of 100.
const DefaultMaxConns int32 = 16

// MaxAllowedConns bounds a single process's pool.
//
// Above this the configuration is almost certainly a mistake: PostgreSQL's
// default max_connections is 100, so one process claiming more than a quarter of
// it would starve every other client of the same database — including the
// migrations and the health checks needed to diagnose the resulting failures.
const MaxAllowedConns int32 = 25

// Connect opens a pool with the default connection budget and verifies
// connectivity.
//
// It is a shorthand for ConnectWithPool, kept because most callers (migrations,
// tests, the migration source adapters) genuinely want one connection budget and
// should not have to name it.
func Connect(ctx context.Context, url string) (*DB, error) {
	return ConnectWithPool(ctx, url, DefaultMaxConns)
}

// ConnectWithPool opens a pool with an explicit connection budget.
//
// maxConns <= 0 selects DefaultMaxConns. A value above MaxAllowedConns is
// rejected rather than silently clamped: a quietly reduced pool would surface
// later as unexplained timeouts under load, which is far harder to diagnose than
// a startup error.
func ConnectWithPool(ctx context.Context, url string, maxConns int32) (*DB, error) {
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}
	if maxConns > MaxAllowedConns {
		return nil, fmt.Errorf(
			"database pool size %d exceeds the maximum of %d; size each replica's "+
				"pool so that replicas x pool stays below the server's max_connections",
			maxConns, MaxAllowedConns)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	// MinConns stays at 1: idle connections are not free, and a floor held by
	// every replica multiplies the same way MaxConns does.
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.RuntimeParams["application_name"] = "forgecrm"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Ping checks that the database is reachable. Used by the readiness probe.
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// Tx is an in-progress transaction.
type Tx = pgx.Tx

// Querier is satisfied by both the pool and a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithTx runs fn inside a transaction, committing on success and rolling back on
// error or panic. Nested calls reuse the ambient transaction from the context.
func (d *DB) WithTx(ctx context.Context, fn func(context.Context, Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(ctx, tx)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Use a detached context so rollback still happens if the caller's
			// context was cancelled.
			rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(rbCtx)
		}
	}()

	if err := fn(WithTx(ctx, tx), tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

type txCtxKey struct{}

// WithTx attaches a transaction to the context.
func WithTx(ctx context.Context, tx Tx) context.Context {
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// TxFromContext returns the ambient transaction, if any.
func TxFromContext(ctx context.Context) (Tx, bool) {
	tx, ok := ctx.Value(txCtxKey{}).(Tx)
	return tx, ok
}

// Q returns the transaction from the context when present, otherwise the pool.
// Domain code uses this so that repositories work both inside and outside a
// transaction without duplicating every method.
func (d *DB) Q(ctx context.Context) Querier {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}
	return d.pool
}

// IsUniqueViolation reports whether err is a PostgreSQL unique-constraint error.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// IsNoRows reports whether err means a query returned no row. It is the sentinel
// pgx returns from QueryRow.Scan, wrapped here so callers outside this package do
// not import the driver directly.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsForeignKeyViolation reports whether err is a foreign-key violation.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// IsCheckViolation reports whether err is a check-constraint violation.
func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514"
	}
	return false
}

// RedactDSN removes sensitive password information from a PostgreSQL DSN.
func RedactDSN(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<invalid DSN>"
	}
	return parsed.Redacted()
}
