//go:build integration

// Integration test for the worker's shutdown drain. It needs a real PostgreSQL
// server because the property under test is about the job row's terminal state:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./cmd/forgecrm-worker/...
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/migrate"
)

// connectOrSkip returns a pool for the test database, or skips when unset so the
// default build stays green.
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

// freshSchema drops and rebuilds the public schema so the test starts from a
// known state.
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

// TestWorkerDrainsInFlightJobOnShutdown is the guard for the drain behaviour. A
// shutdown signal must stop the worker claiming new work but let a job that is
// already running reach a terminal state; a job killed mid-run stays 'running'
// until the requeue sweep notices it, and any write it was in the middle of is
// lost. The run context is checked inside the handler because that is exactly
// what regressed: when the run context derived from the signal context, the
// handler was cancelled the moment the signal arrived.
func TestWorkerDrainsInFlightJobOnShutdown(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	queue := jobs.NewQueue(db, slog.Default())

	job, err := queue.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.drain"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	started := make(chan struct{})
	var once sync.Once
	handler := func(runCtx context.Context, _ *jobs.Queue, _ *jobs.Job) error {
		once.Do(func() { close(started) })
		time.Sleep(300 * time.Millisecond)
		if err := runCtx.Err(); err != nil {
			return fmt.Errorf("run context was cancelled during shutdown: %w", err)
		}
		return nil
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	jobCtx, jobCancel := context.WithCancel(context.Background())
	defer jobCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		workerLoop(loopCtx, jobCtx, slog.Default(), queue, handler, "test-worker", 25*time.Millisecond)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never claimed the job")
	}

	// Shutdown: stop the poll loop while the job is mid-flight.
	loopCancel()

	drained := make(chan struct{})
	go func() {
		wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not drain the in-flight job")
	}

	// The row must be terminal, not left 'running'.
	got, err := queue.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("read the job back: %v", err)
	}
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("job status = %q, want %q (the job was abandoned mid-run)", got.Status, jobs.StatusSucceeded)
	}
}

// TestWorkerStopsClaimingAfterShutdown proves the other half: once the loop
// context is cancelled the worker takes no new job, so a shutdown does not keep
// pulling work it cannot finish.
func TestWorkerStopsClaimingAfterShutdown(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	queue := jobs.NewQueue(db, slog.Default())

	job, err := queue.Enqueue(ctx, jobs.EnqueueRequest{Kind: "test.stop"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	jobCtx, jobCancel := context.WithCancel(context.Background())
	defer jobCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		workerLoop(loopCtx, jobCtx, slog.Default(), queue,
			func(context.Context, *jobs.Queue, *jobs.Job) error { return nil },
			"test-worker", 25*time.Millisecond)
	}()

	loopCancel()
	wg.Wait()

	got, err := queue.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("read the job back: %v", err)
	}
	if got.Status != jobs.StatusQueued {
		t.Fatalf("job status = %q, want %q (the worker claimed work after shutdown)", got.Status, jobs.StatusQueued)
	}
}
