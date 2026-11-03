//go:build integration

package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/logging"
)

// TestWorkerHeartbeatTracksLiveness is the regression test for the blind spot this
// change closes: the worker is a separate service with no health check, so before
// the heartbeat nothing in the platform could answer "is the worker up". The test
// walks the two states that matter — a fresh heartbeat counts, and a stale one does
// not — against a real database, because the aging-out behaviour is the database's
// own `now()` arithmetic and a mock would only confirm the mock.
func TestWorkerHeartbeatTracksLiveness(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()

	queue := jobs.NewQueue(db, logging.New("error", "text"))

	// Nothing has beaten yet, so no worker is alive. This is the state a
	// deployment is in when the worker never started.
	alive, err := jobs.AliveWorkers(ctx, db.Pool(), jobs.HeartbeatWindow)
	if err != nil {
		t.Fatalf("count alive workers on an empty table: %v", err)
	}
	if alive != 0 {
		t.Fatalf("alive workers = %d before any heartbeat, want 0", alive)
	}

	// One worker beats. It must count immediately: the first beat is written
	// synchronously, so a worker that starts and dies within one interval is still
	// visible.
	if err := queue.Beat(ctx, jobs.Heartbeat{
		WorkerID: "worker-a", Role: "worker", Version: "1.2.3", Commit: "abc123",
	}); err != nil {
		t.Fatalf("record a heartbeat: %v", err)
	}
	if alive, err = jobs.AliveWorkers(ctx, db.Pool(), jobs.HeartbeatWindow); err != nil {
		t.Fatalf("count alive workers after a beat: %v", err)
	} else if alive != 1 {
		t.Fatalf("alive workers = %d after one beat, want 1", alive)
	}

	// A second worker is a second row, not an update: several replicas coexist.
	if err := queue.Beat(ctx, jobs.Heartbeat{WorkerID: "worker-b", Role: "worker"}); err != nil {
		t.Fatalf("record a second heartbeat: %v", err)
	}
	if alive, err = jobs.AliveWorkers(ctx, db.Pool(), jobs.HeartbeatWindow); err != nil {
		t.Fatalf("count alive workers after two beats: %v", err)
	} else if alive != 2 {
		t.Fatalf("alive workers = %d after two beats, want 2", alive)
	}

	// A repeated beat for the same id refreshes the one row. The count stays 2,
	// which proves the upsert keys on worker_id rather than inserting a duplicate.
	if err := queue.Beat(ctx, jobs.Heartbeat{
		WorkerID: "worker-a", Role: "worker", Version: "1.2.3", Commit: "abc123",
	}); err != nil {
		t.Fatalf("refresh a heartbeat: %v", err)
	}
	if alive, err = jobs.AliveWorkers(ctx, db.Pool(), jobs.HeartbeatWindow); err != nil {
		t.Fatalf("count alive workers after a refresh: %v", err)
	} else if alive != 2 {
		t.Fatalf("alive workers = %d after refreshing one of two, want 2 (the row was duplicated)", alive)
	}

	// Age one worker past the window by writing its last_seen_at into the past. A
	// worker that died without clearing anything must drop out on its own; this is
	// the whole reason the record is a timestamp.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE worker_heartbeats SET last_seen_at = now() - ($1::interval * 2) WHERE worker_id = 'worker-a'`,
		jobs.HeartbeatWindow.String()); err != nil {
		t.Fatalf("age a heartbeat: %v", err)
	}
	if alive, err = jobs.AliveWorkers(ctx, db.Pool(), jobs.HeartbeatWindow); err != nil {
		t.Fatalf("count alive workers after aging one out: %v", err)
	} else if alive != 1 {
		t.Fatalf("alive workers = %d after one aged out, want 1", alive)
	}

	// The version and commit the worker reported are readable, so an operator can
	// see which build is running without opening the worker's log.
	var version, commit string
	if err := db.Pool().QueryRow(ctx,
		`SELECT version, commit FROM worker_heartbeats WHERE worker_id = 'worker-a'`).
		Scan(&version, &commit); err != nil {
		t.Fatalf("read the reported build: %v", err)
	}
	if version != "1.2.3" || commit != "abc123" {
		t.Fatalf("reported build = %q/%q, want 1.2.3/abc123", version, commit)
	}
}

// TestHeartbeatUpsertPreservesStartedAt checks the one field a restart must not
// overwrite. `started_at` answers "how long has this worker id been running"; if
// every beat reset it, it would be a second copy of last_seen_at and the question
// would be unanswerable.
func TestHeartbeatUpsertPreservesStartedAt(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()

	queue := jobs.NewQueue(db, logging.New("error", "text"))

	if err := queue.Beat(ctx, jobs.Heartbeat{WorkerID: "worker-a", Role: "worker"}); err != nil {
		t.Fatalf("record a heartbeat: %v", err)
	}
	// Push started_at into the past, then beat again. A correct upsert leaves it
	// alone; one that overwrites it on conflict resets it to now().
	if _, err := db.Pool().Exec(ctx,
		`UPDATE worker_heartbeats SET started_at = now() - interval '1 hour' WHERE worker_id = 'worker-a'`); err != nil {
		t.Fatalf("backdate started_at: %v", err)
	}
	if err := queue.Beat(ctx, jobs.Heartbeat{WorkerID: "worker-a", Role: "worker"}); err != nil {
		t.Fatalf("beat again: %v", err)
	}

	var age time.Duration
	if err := db.Pool().QueryRow(ctx,
		`SELECT now() - started_at FROM worker_heartbeats WHERE worker_id = 'worker-a'`).Scan(&age); err != nil {
		t.Fatalf("read started_at: %v", err)
	}
	if age < 30*time.Minute {
		t.Fatalf("started_at was reset by the upsert (age %s); a restart must not overwrite it", age)
	}
}
