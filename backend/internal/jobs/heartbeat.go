package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
)

// This file implements the worker liveness heartbeat. See
// 0031_worker_heartbeats.up.sql for why the record is a timestamp rather than a
// status flag: a timestamp that stops advancing ages out on its own, so a worker
// that died without running its shutdown path leaves the count without anyone
// clearing a row.

// HeartbeatInterval is how often a worker refreshes its liveness row.
//
// It is short enough that a crashed worker drops out of the alive count within a
// scrape or two, and long enough that the write is negligible beside the job
// polling it sits next to.
const HeartbeatInterval = 10 * time.Second

// HeartbeatWindow is how stale a heartbeat may be before its worker is counted as
// gone. It is a small multiple of HeartbeatInterval, so one missed beat — a slow
// write, a paused process — does not read as a dead worker, while a worker that
// has genuinely stopped ages out within a few intervals.
//
// Both values live here, not in the worker and the API separately: they are two
// halves of one threshold, and a deployment that moved one without the other would
// silently misreport liveness.
const HeartbeatWindow = 3 * HeartbeatInterval

// Heartbeat is one worker process's liveness record.
type Heartbeat struct {
	WorkerID string
	Role     string
	Version  string
	Commit   string
}

// Beat records that this worker is alive, creating the row on first use and
// updating it afterwards.
//
// It is an upsert rather than an insert-then-update because a worker restarted on
// the same host reuses its id, and a primary-key conflict must refresh the row,
// not fail the heartbeat. `started_at` is preserved across a restart on purpose:
// it answers "how long has a worker with this id been running", and overwriting it
// every beat would make it a second `last_seen_at`.
func (q *Queue) Beat(ctx context.Context, hb Heartbeat) error {
	if hb.WorkerID == "" {
		return fmt.Errorf("heartbeat: worker id is required")
	}
	_, err := q.db.Q(ctx).Exec(ctx, `
		INSERT INTO worker_heartbeats (worker_id, role, version, commit, started_at, last_seen_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (worker_id) DO UPDATE SET
			role = EXCLUDED.role,
			version = EXCLUDED.version,
			commit = EXCLUDED.commit,
			last_seen_at = now()`,
		hb.WorkerID, hb.Role, hb.Version, hb.Commit)
	if err != nil {
		return fmt.Errorf("record heartbeat: %w", err)
	}
	return nil
}

// RunBeats calls Beat on every interval until ctx is cancelled.
//
// A failed beat is logged and retried on the next tick rather than returning: a
// transient database error must not stop a worker from doing its actual work. The
// first beat is written immediately so a worker that starts and dies within one
// interval is still visible.
func (q *Queue) RunBeats(ctx context.Context, hb Heartbeat, every time.Duration) {
	if every <= 0 {
		every = HeartbeatInterval
	}
	if err := q.Beat(ctx, hb); err != nil {
		q.log.Warn("could not write the initial worker heartbeat", "error", err)
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := q.Beat(ctx, hb); err != nil {
				q.log.Warn("could not write a worker heartbeat", "error", err)
			}
		}
	}
}

// AliveWorkers counts the workers that beat within the window.
//
// The window is the caller's, not this function's: how stale a heartbeat may be
// before a worker is considered gone depends on the beat interval, which only the
// deployment knows. A caller passes a small multiple of that interval.
func AliveWorkers(ctx context.Context, db database.Querier, within time.Duration) (int64, error) {
	if within <= 0 {
		return 0, fmt.Errorf("alive workers: window must be positive")
	}
	var n int64
	err := db.QueryRow(ctx,
		`SELECT count(*) FROM worker_heartbeats WHERE last_seen_at > now() - $1::interval`,
		within.String()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count alive workers: %w", err)
	}
	return n, nil
}
