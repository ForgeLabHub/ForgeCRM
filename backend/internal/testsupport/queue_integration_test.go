//go:build integration

// Integration tests for the queue's delivery semantics against a real PostgreSQL.
// They live in testsupport rather than internal/jobs because they need the
// migration runner and the test-database provisioner, which internal/jobs must
// not depend on — jobs is an extracted module, and importing the monolith from
// its tests would stop it extracting. The properties that matter here are
// properties of the SQL, not of the Go around it: claim exclusivity, retry with
// backoff, the max-attempts bound, and stale recovery. A mock would satisfy every
// one of them by construction.
package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
)

// freshQueue returns a queue over a clean schema on this package's private
// database, so a leftover job from another test cannot satisfy a claim.
func freshQueue(t *testing.T) (*jobs.Queue, *database.DB) {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)
	return jobs.NewQueue(db, slog.Default()), db
}

// TestClaimIsExclusiveAcrossWorkers proves two workers polling at the same
// instant take different jobs and neither sees the other's. This is the property
// FOR UPDATE SKIP LOCKED provides, and the one a second replica depends on.
func TestClaimIsExclusiveAcrossWorkers(t *testing.T) {
	q, _ := freshQueue(t)
	ctx := context.Background()

	const n = 5
	for i := 0; i < n; i++ {
		if _, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.claim"}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	const workers = 8
	claimed := make(chan string, n*workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				job, err := q.Claim(ctx, "worker")
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if job == nil {
					return
				}
				claimed <- job.ID.String()
			}
		}(i)
	}
	wg.Wait()
	close(claimed)

	seen := map[string]int{}
	for id := range claimed {
		seen[id]++
	}
	if len(seen) != n {
		t.Fatalf("%d distinct jobs claimed, want %d (a job was claimed twice or lost)", len(seen), n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("job %s was claimed %d times, want exactly once", id, count)
		}
	}
}

// TestClaimSkipsFutureAndCancelledJobs proves the claim honours run_after and
// cancel_requested: a job scheduled for later or cancelled before it starts must
// not be handed to a worker.
func TestClaimSkipsFutureAndCancelledJobs(t *testing.T) {
	q, _ := freshQueue(t)
	ctx := context.Background()

	later := time.Now().Add(time.Hour)
	if _, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.future", RunAfter: &later}); err != nil {
		t.Fatalf("enqueue future: %v", err)
	}
	cancelled, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.cancelled"})
	if err != nil {
		t.Fatalf("enqueue cancelled: %v", err)
	}
	if err := q.Cancel(ctx, cancelled.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if job, err := q.Claim(ctx, "w"); err != nil {
		t.Fatalf("claim: %v", err)
	} else if job != nil {
		t.Fatalf("claimed job %s (%s) when only a future and a cancelled job were queued", job.ID, job.Kind)
	}
}

// TestFailRetriesWithBackoffThenExhausts proves the retry policy end to end: a
// job that fails is requeued with a future run_after while attempts remain, and is
// marked failed once they are used up. A job that retried forever, or one that
// gave up on the first error, would both be wrong.
func TestFailRetriesWithBackoffThenExhausts(t *testing.T) {
	q, db := freshQueue(t)
	ctx := context.Background()

	job, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.retry", MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// First attempt: claim increments attempts to 1, Fail requeues with backoff.
	claimed, err := q.Claim(ctx, "w")
	if err != nil || claimed == nil {
		t.Fatalf("first claim: %v (%v)", err, claimed)
	}
	if claimed.Attempts != 1 {
		t.Fatalf("attempts after first claim = %d, want 1", claimed.Attempts)
	}
	if err := q.Fail(ctx, claimed, errors.New("transient")); err != nil {
		t.Fatalf("fail: %v", err)
	}
	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != jobs.StatusQueued {
		t.Fatalf("status after a retryable failure = %q, want %q", got.Status, jobs.StatusQueued)
	}
	if !got.RunAfter.After(time.Now()) {
		t.Errorf("run_after = %s, want a future time (the backoff was not applied)", got.RunAfter)
	}

	// Make the retry due now and run the second (final) attempt.
	if _, err := db.Pool().Exec(ctx, `UPDATE jobs SET run_after = now() WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	claimed, err = q.Claim(ctx, "w")
	if err != nil || claimed == nil {
		t.Fatalf("second claim: %v (%v)", err, claimed)
	}
	if claimed.Attempts != 2 {
		t.Fatalf("attempts after second claim = %d, want 2", claimed.Attempts)
	}
	if err := q.Fail(ctx, claimed, errors.New("permanent")); err != nil {
		t.Fatalf("second fail: %v", err)
	}
	got, err = q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != jobs.StatusFailed {
		t.Fatalf("status after exhausting attempts = %q, want %q", got.Status, jobs.StatusFailed)
	}
	if got.LastError == nil || *got.LastError == "" {
		t.Error("a failed job kept no last_error, so an operator cannot see why")
	}
}

// TestRequeueStaleReturnsAbandonedJobs proves a job left 'running' by a worker
// that died is returned to the queue, and that a freshly-claimed job is not.
func TestRequeueStaleReturnsAbandonedJobs(t *testing.T) {
	q, db := freshQueue(t)
	ctx := context.Background()

	abandoned, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.stale"})
	if err != nil {
		t.Fatalf("enqueue abandoned: %v", err)
	}
	// A running job whose lock is old, as a crash leaves it.
	if _, err := db.Pool().Exec(ctx, `
		UPDATE jobs SET status = 'running', locked_by = 'dead', locked_at = now() - interval '30 minutes'
		WHERE id = $1`, abandoned.ID); err != nil {
		t.Fatalf("simulate the stale lock: %v", err)
	}
	// A running job claimed just now must not be disturbed.
	fresh, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.fresh"})
	if err != nil {
		t.Fatalf("enqueue fresh: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		UPDATE jobs SET status = 'running', locked_by = 'alive', locked_at = now()
		WHERE id = $1`, fresh.ID); err != nil {
		t.Fatalf("simulate the fresh lock: %v", err)
	}

	n, err := q.RequeueStale(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("requeue stale: %v", err)
	}
	if n != 1 {
		t.Errorf("requeued %d jobs, want 1", n)
	}
	got, err := q.Get(ctx, abandoned.ID)
	if err != nil {
		t.Fatalf("read abandoned: %v", err)
	}
	if got.Status != jobs.StatusQueued {
		t.Errorf("abandoned job status = %q, want %q", got.Status, jobs.StatusQueued)
	}
	freshGot, err := q.Get(ctx, fresh.ID)
	if err != nil {
		t.Fatalf("read fresh: %v", err)
	}
	if freshGot.Status != jobs.StatusRunning {
		t.Errorf("freshly-claimed job status = %q, want %q (a live claim was reclaimed)", freshGot.Status, jobs.StatusRunning)
	}
}

// TestCompleteAndCancelAreTerminal proves the two other terminal transitions.
func TestCompleteAndCancelAreTerminal(t *testing.T) {
	q, db := freshQueue(t)
	ctx := context.Background()

	job, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.complete"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := q.Claim(ctx, "w")
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if err := q.Complete(ctx, claimed.ID, map[string]any{"ok": true}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != jobs.StatusSucceeded {
		t.Errorf("status after complete = %q, want %q", got.Status, jobs.StatusSucceeded)
	}
	// A completed job must release its claim, or a stale sweep could re-run it. The
	// Job type does not expose the lock columns, so the check reads them directly.
	var lockedBy *string
	var lockedAt *time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT locked_by, locked_at FROM jobs WHERE id = $1`, job.ID).Scan(&lockedBy, &lockedAt); err != nil {
		t.Fatalf("read the lock columns: %v", err)
	}
	if lockedBy != nil || lockedAt != nil {
		t.Errorf("a completed job kept its claim (locked_by=%v locked_at=%v)", lockedBy, lockedAt)
	}

	// Cancel is the other terminal transition.
	other, err := q.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.cancel"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := q.Cancel(ctx, other.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	otherGot, err := q.Get(ctx, other.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if otherGot.Status != jobs.StatusCancelled {
		t.Errorf("status after cancel = %q, want %q", otherGot.Status, jobs.StatusCancelled)
	}
}
