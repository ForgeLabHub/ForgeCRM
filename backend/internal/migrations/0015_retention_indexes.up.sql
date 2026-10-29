-- 0015_retention_indexes: indexes that let the retention sweep find expired rows
-- without scanning a table.
--
-- The sweep in internal/retention deletes `WHERE <time_column> < cutoff` on each
-- policy's table. Without an index on that column the statement is a sequential
-- scan, and the larger the table the slower the sweep — which is exactly backwards,
-- because the table is large because nothing pruned it.
--
-- Each index is partial where the sweep is partial, so it covers only the rows the
-- sweep can ever look at. That keeps the index small: a jobs index over finished
-- jobs is a fraction of a jobs index over every job, and the sweep never reads the
-- others.
--
-- CONCURRENTLY is not used: migrations run inside a transaction, and CREATE INDEX
-- CONCURRENTLY cannot. On a fresh or small deployment the lock is momentary; on a
-- large one the index build is still far cheaper than the scans it removes.

-- webhook_deliveries: delivered receipts and dead letters.
CREATE INDEX IF NOT EXISTS webhook_deliveries_delivered_at_idx
    ON webhook_deliveries (delivered_at) WHERE status = 'delivered';
CREATE INDEX IF NOT EXISTS webhook_deliveries_dead_at_idx
    ON webhook_deliveries (next_attempt_at) WHERE status = 'dead';

-- jobs: only finished jobs are swept; a queued or running job is never eligible.
CREATE INDEX IF NOT EXISTS jobs_finished_at_idx
    ON jobs (finished_at) WHERE status IN ('succeeded', 'cancelled');

-- ai_tasks: the AI call log, swept by when the call finished.
CREATE INDEX IF NOT EXISTS ai_tasks_finished_at_idx ON ai_tasks (finished_at);

-- integration_runs: a finished run is a sync receipt.
CREATE INDEX IF NOT EXISTS integration_runs_finished_at_idx ON integration_runs (finished_at);

-- import_jobs and export_jobs: swept only once terminal.
CREATE INDEX IF NOT EXISTS import_jobs_finished_at_idx
    ON import_jobs (finished_at) WHERE status IN ('succeeded', 'failed', 'cancelled');
CREATE INDEX IF NOT EXISTS export_jobs_finished_at_idx
    ON export_jobs (finished_at) WHERE status IN ('succeeded', 'failed', 'cancelled');

-- notifications: only a read notification is swept.
CREATE INDEX IF NOT EXISTS notifications_sweep_idx
    ON notifications (created_at) WHERE read_at IS NOT NULL;

-- outbox_events: only a published event is swept. The existing outbox_pending_idx
-- covers the opposite set (unpublished) and does not help this sweep.
CREATE INDEX IF NOT EXISTS outbox_events_published_at_idx
    ON outbox_events (occurred_at) WHERE published_at IS NOT NULL;

-- sessions: an expired session is no longer accepted.
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);
