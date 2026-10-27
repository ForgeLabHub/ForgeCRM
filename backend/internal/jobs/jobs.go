// Package jobs implements a PostgreSQL-backed background job queue.
//
// Postgres is used instead of an external broker because it removes a mandatory
// moving part from the deployment while still giving durable, transactional
// enqueueing: a job can be inserted in the same transaction as the data it
// describes, so a crash cannot leave the two out of step.
//
// Claiming uses FOR UPDATE SKIP LOCKED, so multiple workers can run concurrently
// without contending or double-processing.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/database"
)

// Status values for a job.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Job kinds registered in the worker.
const (
	KindImportExecute  = "import.execute"
	KindImportDryRun   = "import.dry_run"
	KindExportExecute  = "export.execute"
	KindMigrationRun   = "migration.run"
	KindWebhookDeliver = "webhook.deliver"
	KindEmailSend      = "email.send"
	KindOutboxDrain    = "outbox.drain"
	KindReconcile      = "migration.reconcile"
	KindRetentionSweep = "maintenance.retention"
)

// Job is a queued unit of work.
type Job struct {
	ID              uuid.UUID       `json:"id"`
	OrganizationID  *uuid.UUID      `json:"organization_id,omitempty"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Priority        int             `json:"priority"`
	Payload         json.RawMessage `json:"payload"`
	Checkpoint      json.RawMessage `json:"checkpoint,omitempty"`
	Progress        json.RawMessage `json:"progress,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Attempts        int             `json:"attempts"`
	MaxAttempts     int             `json:"max_attempts"`
	RunAfter        time.Time       `json:"run_after"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	LastError       *string         `json:"last_error,omitempty"`
	CancelRequested bool            `json:"cancel_requested"`
	CreatedBy       *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Queue enqueues and manages jobs.
type Queue struct {
	db  *database.DB
	log *slog.Logger
}

// NewQueue builds a queue.
func NewQueue(db *database.DB, log *slog.Logger) *Queue {
	return &Queue{db: db, log: log}
}

// EnqueueRequest describes a job to create.
type EnqueueRequest struct {
	OrganizationID *uuid.UUID
	Kind           string
	Payload        any
	Priority       int
	MaxAttempts    int
	RunAfter       *time.Time
	CreatedBy      *uuid.UUID
}

// Enqueue inserts a job. When called inside a transaction, the insert joins that
// transaction so the job and the data it refers to commit together.
func (q *Queue) Enqueue(ctx context.Context, req EnqueueRequest) (*Job, error) {
	if req.Kind == "" {
		return nil, errors.New("job kind is required")
	}
	payload, err := json.Marshal(nonNil(req.Payload))
	if err != nil {
		return nil, fmt.Errorf("marshal job payload: %w", err)
	}
	if req.Priority == 0 {
		req.Priority = 100
	}
	if req.MaxAttempts <= 0 {
		req.MaxAttempts = 5
	}
	runAfter := time.Now()
	if req.RunAfter != nil {
		runAfter = *req.RunAfter
	}
	const sql = `
		INSERT INTO jobs (organization_id, kind, payload, priority, max_attempts, run_after, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + jobColumns
	row := q.db.Q(ctx).QueryRow(ctx, sql,
		req.OrganizationID, req.Kind, payload, req.Priority, req.MaxAttempts, runAfter, req.CreatedBy)
	return scanJob(row)
}

const jobColumns = `
	id, organization_id, kind, status, priority, payload, checkpoint, progress, result,
	attempts, max_attempts, run_after, started_at, finished_at, last_error,
	cancel_requested, created_by, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*Job, error) {
	var j Job
	err := row.Scan(
		&j.ID, &j.OrganizationID, &j.Kind, &j.Status, &j.Priority, &j.Payload, &j.Checkpoint,
		&j.Progress, &j.Result, &j.Attempts, &j.MaxAttempts, &j.RunAfter, &j.StartedAt,
		&j.FinishedAt, &j.LastError, &j.CancelRequested, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// Claim atomically takes the next eligible job for a worker.
//
// The statement marks the row running in the same statement that selects it, so
// two workers cannot both claim it. Returns nil when nothing is ready.
func (q *Queue) Claim(ctx context.Context, workerID string) (*Job, error) {
	const sql = `
		UPDATE jobs SET
			status = 'running',
			attempts = attempts + 1,
			locked_at = now(),
			locked_by = $1,
			started_at = COALESCE(started_at, now())
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'queued' AND run_after <= now() AND NOT cancel_requested
			ORDER BY priority ASC, run_after ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING ` + jobColumns
	job, err := scanJob(q.db.Pool().QueryRow(ctx, sql, workerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	return job, nil
}

// Complete marks a job succeeded and stores its result.
func (q *Queue) Complete(ctx context.Context, jobID uuid.UUID, result any) error {
	payload, err := json.Marshal(nonNil(result))
	if err != nil {
		return fmt.Errorf("marshal job result: %w", err)
	}
	const sql = `
		UPDATE jobs SET status = 'succeeded', result = $2, finished_at = now(),
		                locked_at = NULL, locked_by = NULL, last_error = NULL
		WHERE id = $1`
	if _, err := q.db.Pool().Exec(ctx, sql, jobID, payload); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return nil
}

// Fail records a failure. If attempts remain, the job is requeued with
// exponential backoff; otherwise it is marked failed.
func (q *Queue) Fail(ctx context.Context, job *Job, cause error) error {
	msg := cause.Error()
	if len(msg) > 4000 {
		msg = msg[:4000]
	}
	if job.Attempts < job.MaxAttempts {
		backoff := backoffFor(job.Attempts)
		const sql = `
			UPDATE jobs SET status = 'queued', last_error = $2, run_after = $3,
			                locked_at = NULL, locked_by = NULL
			WHERE id = $1`
		if _, err := q.db.Pool().Exec(ctx, sql, job.ID, msg, time.Now().Add(backoff)); err != nil {
			return fmt.Errorf("requeue job: %w", err)
		}
		q.log.Warn("job failed, will retry",
			"job_id", job.ID, "kind", job.Kind, "attempt", job.Attempts, "backoff", backoff, "error", msg)
		return nil
	}
	const sql = `
		UPDATE jobs SET status = 'failed', last_error = $2, finished_at = now(),
		                locked_at = NULL, locked_by = NULL
		WHERE id = $1`
	if _, err := q.db.Pool().Exec(ctx, sql, job.ID, msg); err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}
	q.log.Error("job exhausted retries", "job_id", job.ID, "kind", job.Kind, "error", msg)
	return nil
}

// backoffFor returns an exponential backoff capped at 30 minutes.
func backoffFor(attempt int) time.Duration {
	d := time.Duration(1<<uint(min(attempt, 10))) * time.Second
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SaveCheckpoint persists resumable progress for a long-running job.
func (q *Queue) SaveCheckpoint(ctx context.Context, jobID uuid.UUID, checkpoint, progress any) error {
	cp, err := json.Marshal(nonNil(checkpoint))
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	pr, err := json.Marshal(nonNil(progress))
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}
	const sql = `UPDATE jobs SET checkpoint = $2, progress = $3 WHERE id = $1`
	if _, err := q.db.Pool().Exec(ctx, sql, jobID, cp, pr); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	return nil
}

// Get loads a single job.
func (q *Queue) Get(ctx context.Context, id uuid.UUID) (*Job, error) {
	const sql = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1`
	job, err := scanJob(q.db.Q(ctx).QueryRow(ctx, sql, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load job: %w", err)
	}
	return job, nil
}

// Cancel requests cancellation. A running job observes the flag through
// IsCancelled; a queued job is cancelled immediately.
func (q *Queue) Cancel(ctx context.Context, id uuid.UUID) error {
	const sql = `
		UPDATE jobs SET
			cancel_requested = true,
			status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE status END,
			finished_at = CASE WHEN status = 'queued' THEN now() ELSE finished_at END
		WHERE id = $1 AND status IN ('queued', 'running')`
	if _, err := q.db.Pool().Exec(ctx, sql, id); err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	return nil
}

// Retry requeues a finished job, resetting its attempt counter.
func (q *Queue) Retry(ctx context.Context, id uuid.UUID) error {
	const sql = `
		UPDATE jobs SET status = 'queued', attempts = 0, run_after = now(),
		                cancel_requested = false, last_error = NULL,
		                finished_at = NULL, started_at = NULL, locked_at = NULL, locked_by = NULL
		WHERE id = $1 AND status IN ('failed', 'cancelled', 'succeeded')`
	tag, err := q.db.Pool().Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotRetryable
	}
	return nil
}

// ErrNotRetryable is returned when a job cannot be retried in its current state.
var ErrNotRetryable = errors.New("job is not in a retryable state")

// IsCancelled reports whether cancellation has been requested.
func (q *Queue) IsCancelled(ctx context.Context, id uuid.UUID) (bool, error) {
	const sql = `SELECT cancel_requested FROM jobs WHERE id = $1`
	var cancelled bool
	err := q.db.Pool().QueryRow(ctx, sql, id).Scan(&cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check cancellation: %w", err)
	}
	return cancelled, nil
}

// List returns jobs for an organization, optionally filtered by status and kind.
func (q *Queue) List(ctx context.Context, orgID *uuid.UUID, status, kind string, limit, offset int) ([]Job, int64, error) {
	args := []any{}
	where := "WHERE 1 = 1"
	if orgID != nil {
		args = append(args, *orgID)
		where += fmt.Sprintf(" AND organization_id = $%d", len(args))
	}
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if kind != "" {
		args = append(args, kind)
		where += fmt.Sprintf(" AND kind = $%d", len(args))
	}

	var total int64
	countSQL := "SELECT count(*) FROM jobs " + where
	if err := q.db.Q(ctx).QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	args = append(args, limit, offset)
	listSQL := `SELECT ` + jobColumns + ` FROM jobs ` + where +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := q.db.Q(ctx).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *j)
	}
	return out, total, rows.Err()
}

// RequeueStale returns jobs abandoned by a crashed worker to the queue. Called at
// worker start so that an unclean shutdown does not strand work.
func (q *Queue) RequeueStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	const sql = `
		UPDATE jobs SET status = 'queued', locked_at = NULL, locked_by = NULL
		WHERE status = 'running' AND locked_at < now() - $1::interval`
	tag, err := q.db.Pool().Exec(ctx, sql, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("requeue stale jobs: %w", err)
	}
	if n := tag.RowsAffected(); n > 0 {
		q.log.Warn("requeued stale jobs", "count", n)
	}
	return tag.RowsAffected(), nil
}

// Stats summarises queue depth by status, for the admin diagnostics page.
func (q *Queue) Stats(ctx context.Context, orgID *uuid.UUID) (map[string]int64, error) {
	args := []any{}
	where := ""
	if orgID != nil {
		args = append(args, *orgID)
		where = " WHERE organization_id = $1"
	}
	sql := "SELECT status, count(*) FROM jobs" + where + " GROUP BY status"
	rows, err := q.db.Q(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("job stats: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}

func nonNil(v any) any {
	if v == nil {
		return map[string]any{}
	}
	return v
}
