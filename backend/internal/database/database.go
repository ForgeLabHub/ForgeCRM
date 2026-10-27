// Package database manages the PostgreSQL connection pool and transactions.
package database

import (
	"context"
	"errors"
	"fmt"
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

// Connect opens a pool and verifies connectivity.
func Connect(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 16
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
