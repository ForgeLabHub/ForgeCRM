-- 0017_retention_sweep_indexes: indexes that let the retention sweep cover the
-- workflow and agent run history without scanning those tables.
--
-- Migration 0015 indexed the operational tables the sweep already covered. The
-- sweep now also covers the run history — workflow runs, the agent run log, and
-- the inbound event inbox — because those tables grow without bound on a busy
-- deployment and nothing pruned them. Each index is partial where the sweep is
-- partial, so it covers only the rows the sweep can ever look at and stays small
-- next to the table.
--
-- The sweep's eligibility rule for these tables is "terminal status", so the
-- partial predicate matches the CHECK constraint's terminal values exactly. A
-- running, paused or waiting run is never eligible and is never in the index.
--
-- A workflow run's step records and event claims cascade when the run is deleted,
-- so there is deliberately no separate policy or index for them: sweeping them
-- directly by their own timestamp would remove the history of a run that is still
-- paused and can still resume.
--
-- CONCURRENTLY is not used: migrations run inside a transaction, and CREATE INDEX
-- CONCURRENTLY cannot.

-- workflow_runs: a run that has finished is history; a running or paused run is
-- still in flight and is never swept.
CREATE INDEX IF NOT EXISTS workflow_runs_finished_at_idx
    ON workflow_runs (finished_at)
    WHERE status IN ('succeeded', 'failed', 'skipped');

-- agent_runs: the human-in-the-loop run log, swept by when the run finished. Its
-- actions and their approvals cascade with it.
CREATE INDEX IF NOT EXISTS agent_runs_finished_at_idx
    ON agent_runs (finished_at)
    WHERE status IN ('completed', 'failed', 'cancelled');

-- events_inbox: an inbound event that has been processed or ignored is a receipt.
-- A received or failed event is still pending and is never swept. The existing
-- events_inbox_status_idx leads with status, not received_at, so it does not serve
-- a sweep bounded by time.
CREATE INDEX IF NOT EXISTS events_inbox_processed_idx
    ON events_inbox (received_at)
    WHERE status IN ('processed', 'ignored');
