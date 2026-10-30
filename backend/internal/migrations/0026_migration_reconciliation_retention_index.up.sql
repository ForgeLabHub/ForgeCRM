-- 0026_migration_reconciliation_retention_index: the sweep index the migration
-- reconciliation history needs, following the rule migrations 0017 and 0021 set
-- for the other run histories.
--
-- migration_reconciliations records one row per row-count comparison, written by
-- migratex.Reconcile after an import. Nothing pruned it, so on a deployment that
-- reconciles on a schedule it grows by one row per run without bound. The
-- retention registry now carries a policy for it; this migration gives the sweep
-- an index it can use.
--
-- The sweep deletes rows with a single predicate — created_at against a cutoff —
-- and no organization filter, so it needs an index that leads with created_at.
-- The existing migration_recon_job_idx leads with migration_job_id (it serves the
-- per-job list view), so it cannot serve a sweep bounded by time alone and the
-- sweep would scan the table.
--
-- CONCURRENTLY is not used: migrations run inside a transaction, and CREATE INDEX
-- CONCURRENTLY cannot.

CREATE INDEX IF NOT EXISTS migration_recon_created_at_idx
    ON migration_reconciliations (created_at);
