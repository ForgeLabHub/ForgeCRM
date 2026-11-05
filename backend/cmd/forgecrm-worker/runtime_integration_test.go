//go:build integration

// This file tests the worker the way a deployment runs it: a real compiled
// binary, started as a separate process with a deployment-like environment,
// pointed at a real PostgreSQL database. It is deliberately not a call into
// workerLoop or handle — those are exercised by shutdown_test.go and
// telegram_integration_test.go. The property this file exists to prove is that
// the artefact that ships actually starts, connects, claims a job, runs it, and
// stops on a signal. A unit test of the loop cannot see a missing binary, a bad
// entrypoint, a configuration the process cannot load, or a claim query that
// never matches.
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./cmd/forgecrm-worker/...
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/testdb"
)

// The binary is built once per test run: `go build` is the expensive part and the
// process under test is identical across tests.
var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

// workerBinary compiles the real worker and returns the path to the executable.
//
// It runs `go build` from the package directory, so the result is exactly what a
// release build produces for this package (the release image adds -trimpath and
// ldflags, which change metadata but not behaviour). A build failure fails the
// test rather than being skipped: if the worker does not compile, the suite must
// say so.
func workerBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "forgecrm-worker-bin-")
		if err != nil {
			buildErr = fmt.Errorf("create the build directory: %w", err)
			return
		}
		bin := filepath.Join(dir, "forgecrm-worker")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/forgecrm-worker: %w\n%s", err, out)
			return
		}
		builtPath = bin
	})
	if buildErr != nil {
		t.Fatalf("build the worker binary: %v", buildErr)
	}
	return builtPath
}

// workerEnv builds the child environment from the parent's, minus every
// FORGECRM_ variable, then applies the overrides. Stripping the inherited
// FORGECRM_* matters: the parent test process has FORGECRM_TEST_DATABASE_URL and
// possibly others set, and a child that silently inherited a production-looking
// FORGECRM_DATABASE_URL is exactly the "test pointed at the wrong database" failure
// this whole phase is about.
func workerEnv(overrides map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "FORGECRM_") {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}

// workerProcess is a running worker binary and the file it logs to.
type workerProcess struct {
	cmd    *exec.Cmd
	log    string
	waited bool
}

// startWorker launches the binary as a child process with the given environment,
// capturing its combined output to a file so a failing assertion can print it.
func startWorker(t *testing.T, bin string, env map[string]string) *workerProcess {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "worker.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create the worker log file: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Env = workerEnv(env)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start the worker binary: %v", err)
	}
	// The child holds its own descriptor; the parent's is no longer needed.
	_ = logFile.Close()

	p := &workerProcess{cmd: cmd, log: logPath}
	t.Cleanup(func() {
		if p.waited || p.cmd.Process == nil {
			return
		}
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	})
	return p
}

// read returns the worker's log so far.
func (p *workerProcess) read() string {
	b, _ := os.ReadFile(p.log)
	return string(b)
}

// waitForLog blocks until the log contains substr or the timeout elapses.
func (p *workerProcess) waitForLog(t *testing.T, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(p.read(), substr) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("the worker log never contained %q within %s; log follows:\n%s", substr, timeout, p.read())
}

// signal sends a signal to the process.
func (p *workerProcess) signal(sig syscall.Signal) error {
	return p.cmd.Process.Signal(sig)
}

// waitExit waits for the process to terminate and returns its exit code. A
// non-zero exit code is a result, not an error: the caller decides whether it is
// acceptable. An error means the process did not exit within the timeout.
func (p *workerProcess) waitExit(timeout time.Duration) (int, error) {
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		p.waited = true
		if err == nil {
			return 0, nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return -1, err
	case <-time.After(timeout):
		return -1, fmt.Errorf("the worker did not exit within %s", timeout)
	}
}

// workerRuntimeEnv is the deployment-like environment shared by the runtime
// tests. It sets only what the worker needs to start and reach the database; the
// optional subsystems (SMTP, Telegram, AI, storage) stay off, as they are on a
// default deployment.
func workerRuntimeEnv(dbURL string, extra map[string]string) map[string]string {
	env := map[string]string{
		"FORGECRM_DATABASE_URL":       dbURL,
		"FORGECRM_SECRET_KEY":         "runtime-test-secret-key-000000000000000000000000",
		"FORGECRM_ENV":                "development",
		"FORGECRM_LOG_LEVEL":          "info",
		"FORGECRM_LOG_FORMAT":         "text",
		"FORGECRM_WORKER_CONCURRENCY": "1",
		"FORGECRM_WORKER_POLL":        "100ms",
		"FORGECRM_WORKER_SCHEMA_WAIT": "30s",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// TestWorkerBinaryExecutesAJobFromTheDatabase is the end-to-end proof for the
// worker runtime. It builds the binary, starts it as a separate process against a
// private database, enqueues a real job through the same queue the API uses,
// waits for the job to reach a terminal state, and checks the side effect the
// handler promises. It then sends SIGTERM and requires the process to stop
// claiming work and exit cleanly.
func TestWorkerBinaryExecutesAJobFromTheDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("the real-binary runtime test is skipped in short mode")
	}
	db := testdb.Connect(t) // skips when FORGECRM_TEST_DATABASE_URL is unset
	freshSchema(t, db)
	bin := workerBinary(t)

	// A stale extraction directory, as an import that died would leave behind. The
	// maintenance.import_temp handler removes entries named forgecrm-import-* older
	// than two hours, so this is a real, observable side effect rather than a
	// status flip: after the job runs the directory must be gone.
	tempDir := t.TempDir()
	stale := filepath.Join(tempDir, "forgecrm-import-stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatalf("create the stale extraction directory: %v", err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("age the stale extraction directory: %v", err)
	}

	worker := startWorker(t, bin, workerRuntimeEnv(testdb.URL(t), map[string]string{
		"FORGECRM_IMPORT_TEMP_DIR": tempDir,
	}))
	worker.waitForLog(t, "database schema readiness check passed", 30*time.Second)

	// Enqueue through the real queue, not a raw INSERT, so the test uses the same
	// path the API does.
	ctx := context.Background()
	queue := jobs.NewQueue(db, slog.Default())
	job, err := queue.Enqueue(ctx, jobs.EnqueueRequest{
		Kind:        jobs.KindImportTempSweep,
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue the job: %v", err)
	}

	got := waitForTerminal(t, queue, job.ID, 30*time.Second)
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("job status = %q, want %q; worker log follows:\n%s",
			got.Status, jobs.StatusSucceeded, worker.read())
	}
	if got.Attempts != 1 {
		t.Errorf("job attempts = %d, want 1 (the job should run exactly once)", got.Attempts)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the maintenance sweep left %s in place (stat error: %v); "+
			"the job reported success but did not do its work", stale, err)
	}

	// The real process must have recorded a liveness heartbeat; without it the
	// workers_alive gauge cannot see the worker.
	var beats int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM worker_heartbeats WHERE role = 'worker'`).Scan(&beats); err != nil {
		t.Fatalf("read worker heartbeats: %v", err)
	}
	if beats == 0 {
		t.Error("the worker process recorded no heartbeat")
	}

	// Shutdown: the process must stop claiming and exit cleanly.
	if err := worker.signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	worker.waitForLog(t, "shutdown signal received", 10*time.Second)

	// Enqueue after the shutdown is observable. loopCancel runs immediately after
	// that log line, and this INSERT takes far longer than that, so the worker has
	// already stopped claiming by the time the row exists. A job it claims now is a
	// job it cannot finish, which is the failure the loop context exists to prevent.
	late, err := queue.Enqueue(ctx, jobs.EnqueueRequest{Kind: jobs.KindImportTempSweep, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue the late job: %v", err)
	}

	code, err := worker.waitExit(15 * time.Second)
	if err != nil {
		t.Fatalf("%v; worker log follows:\n%s", err, worker.read())
	}
	if code != 0 {
		t.Errorf("worker exit code = %d, want 0; worker log follows:\n%s", code, worker.read())
	}

	lateGot, err := queue.Get(ctx, late.ID)
	if err != nil {
		t.Fatalf("read the late job: %v", err)
	}
	if lateGot.Status != jobs.StatusQueued {
		t.Errorf("a job enqueued after SIGTERM is %q, want %q (the worker claimed work after shutdown)",
			lateGot.Status, jobs.StatusQueued)
	}
}

// TestWorkerBinaryRecoversAJobFromACrashedWorker is the crash-recovery scenario at
// the process level. One worker claims a job and is then SIGKILLed mid-run, the
// way a container that is OOM-killed or a node that reboots would die. The job's
// row is left 'running'. A second worker process starts, runs the stale-requeue
// sweep, and completes the job. Nothing in-process participates: the recovery is
// the real binary reading the real row.
func TestWorkerBinaryRecoversAJobFromACrashedWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("the real-binary runtime test is skipped in short mode")
	}
	db := testdb.Connect(t)
	freshSchema(t, db)
	bin := workerBinary(t)

	ctx := context.Background()
	queue := jobs.NewQueue(db, slog.Default())

	// Simulate the aftermath of a crash: a job left 'running' with a lock old
	// enough that the requeue sweep will reclaim it. Writing the row directly is
	// the point — the crashed worker cannot do it for us, and this is exactly the
	// state a crash leaves behind.
	job, err := queue.Enqueue(ctx, jobs.EnqueueRequest{Kind: jobs.KindImportTempSweep, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		UPDATE jobs SET status = 'running', locked_by = 'crashed-worker',
		                locked_at = now() - interval '30 minutes', started_at = now() - interval '30 minutes'
		WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("simulate the crashed claim: %v", err)
	}

	// The requeue loop recovers stale jobs once at startup, which is what a
	// restarted worker does, so the second process reclaims this row promptly. The
	// test asserts the outcome (the job completes), not the timing.
	worker := startWorker(t, bin, workerRuntimeEnv(testdb.URL(t), map[string]string{
		"FORGECRM_IMPORT_TEMP_DIR": t.TempDir(),
	}))
	worker.waitForLog(t, "database schema readiness check passed", 30*time.Second)

	got := waitForTerminal(t, queue, job.ID, 60*time.Second)
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("recovered job status = %q, want %q; worker log follows:\n%s",
			got.Status, jobs.StatusSucceeded, worker.read())
	}

	if err := worker.signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	if code, err := worker.waitExit(15 * time.Second); err != nil {
		t.Fatalf("%v", err)
	} else if code != 0 {
		t.Errorf("worker exit code = %d, want 0", code)
	}
}

// TestWorkerBinaryDrainsOutboxIntoDeliveries is the outbox critical-path E2E,
// driven by the real worker process. It emits a domain event the way a committed
// write does, starts the binary, and requires the worker's outbox loop to publish
// the event and the dispatcher to create a webhook delivery for it. This is the
// path that was silently broken before the delivery loop existed: events were
// recorded and nothing consumed them.
//
// The delivery loop then attempts the outbound POST. The target here is a
// loopback address, which the production SSRF guard correctly refuses, so the
// delivery moves to the retrying state. That is expected and is not what this
// test asserts; the property under test is that the event reached the delivery
// table with the right shape. Webhook delivery to a local sink is covered by the
// in-process tests, which can inject a plain poster the real binary must not use.
func TestWorkerBinaryDrainsOutboxIntoDeliveries(t *testing.T) {
	if testing.Short() {
		t.Skip("the real-binary runtime test is skipped in short mode")
	}
	db := testdb.Connect(t)
	freshSchema(t, db)
	bin := workerBinary(t)

	const secretKey = "runtime-test-secret-key-000000000000000000000000"
	ctx := context.Background()

	keyring, err := secrets.NewKeyring(secretKey)
	if err != nil {
		t.Fatalf("build the keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt the subscription secret: %v", err)
	}

	var orgID uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ('Outbox E2E', 'outbox-e2e') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert the organization: %v", err)
	}
	// An empty event_types array matches every event, so the subscription is a
	// catch-all; the target is loopback, which the guard refuses.
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types, secret_encrypted, enabled)
		VALUES ($1, 'sink', 'http://127.0.0.1:1/hook', '{}', $2, true)`, orgID, enc); err != nil {
		t.Fatalf("insert the subscription: %v", err)
	}

	// Emit the event the way a committed CRM write does, through the outbox store.
	if err := outbox.NewStore(db).Emit(ctx, &orgID, outbox.EventContactCreated,
		map[string]any{"id": "00000000-0000-0000-0000-000000000001"}); err != nil {
		t.Fatalf("emit the event: %v", err)
	}

	worker := startWorker(t, bin, workerRuntimeEnv(testdb.URL(t), map[string]string{
		"FORGECRM_SECRET_KEY": secretKey,
	}))
	worker.waitForLog(t, "database schema readiness check passed", 30*time.Second)

	// The event must be published, and a delivery must exist for it.
	deadline := time.Now().Add(30 * time.Second)
	var deliveryCount int
	for time.Now().Before(deadline) {
		if err := db.Pool().QueryRow(ctx,
			`SELECT count(*) FROM webhook_deliveries WHERE organization_id = $1 AND event_type = $2`,
			orgID, outbox.EventContactCreated).Scan(&deliveryCount); err != nil {
			t.Fatalf("count deliveries: %v", err)
		}
		if deliveryCount > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if deliveryCount == 0 {
		t.Fatalf("the worker did not turn the outbox event into a webhook delivery; log follows:\n%s", worker.read())
	}

	// The delivery row is written before the event is marked published, so poll for
	// the flag rather than reading once: a single read races the mark and fails
	// intermittently even though the drain is correct.
	var published bool
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.Pool().QueryRow(ctx, `
			SELECT published_at IS NOT NULL FROM outbox_events WHERE organization_id = $1 AND event_type = $2`,
			orgID, outbox.EventContactCreated).Scan(&published); err != nil {
			t.Fatalf("read the event's published state: %v", err)
		}
		if published {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !published {
		t.Error("the outbox event was dispatched but never marked published, so every drain would re-dispatch it")
	}

	if err := worker.signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	if code, err := worker.waitExit(15 * time.Second); err != nil {
		t.Fatalf("%v", err)
	} else if code != 0 {
		t.Errorf("worker exit code = %d, want 0", code)
	}
}

// waitForTerminal polls the job until it reaches a terminal status or the timeout
// elapses.
func waitForTerminal(t *testing.T, queue *jobs.Queue, id uuid.UUID, timeout time.Duration) *jobs.Job {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		got, err := queue.Get(ctx, id)
		if err != nil {
			t.Fatalf("read job %s: %v", id, err)
		}
		if got != nil && (got.Status == jobs.StatusSucceeded ||
			got.Status == jobs.StatusFailed || got.Status == jobs.StatusCancelled) {
			return got
		}
		if time.Now().After(deadline) {
			status := "<missing>"
			if got != nil {
				status = got.Status
			}
			t.Fatalf("job %s did not reach a terminal state within %s (status %q)", id, timeout, status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
