-- 0021_intelligence_retention_indexes: the sweep indexes the intelligence
-- history needs, following the rule migration 0017 set for the run history.
--
-- The retention sweep deletes rows with a single predicate — the time column
-- against a cutoff, plus the status filter where one exists — and no organization
-- filter, so it needs an index that leads with that time column. The existing
-- indexes on these tables lead with organization_id (they serve the per-org list
-- views), so they cannot serve a sweep bounded by time alone and the sweep would
-- scan the table. On a deployment where the labs have been used for a while that
-- table is the largest one the sweep touches.
--
-- risk_signals is deliberately absent: migration 0020 already created
-- risk_signals_terminal_idx, a partial index on (status_changed_at) restricted to
-- the terminal statuses, for exactly this sweep.
--
-- CONCURRENTLY is not used: migrations run inside a transaction, and CREATE INDEX
-- CONCURRENTLY cannot.

-- simulation_runs: every stored run is finished when written, so the sweep is
-- bounded only by created_at.
CREATE INDEX IF NOT EXISTS simulation_runs_created_at_idx
    ON simulation_runs (created_at);

-- forecast_runs: likewise, a stored forecast run is complete on insert.
CREATE INDEX IF NOT EXISTS forecast_runs_created_at_idx
    ON forecast_runs (created_at);
