-- 0037_telegram_retention_indexes: sweep indexes for the Telegram receipt
-- ledgers, following the rule migrations 0017, 0021, 0024 and 0026 set for the
-- other run histories.
--
-- Two tables grow by one row per Telegram interaction and were never pruned:
--
--   * telegram_update_receipts — one row per incoming webhook update. It is the
--     anti-replay ledger, so the row must survive as long as Telegram may
--     redeliver the update, but it does not have to survive forever. The worker
--     nulls an encrypted reply body older than seven days; nothing removed the
--     row. The existing telegram_updates_pending_age_idx is partial (it covers
--     only rows whose reply_ciphertext IS NOT NULL), so it cannot serve a sweep
--     that looks at finished rows.
--   * telegram_task_delivery_receipts — one row per task notification. It is the
--     duplicate guard for a redelivered outbox event; the row is written with
--     the job and finished when the job finishes, and nothing removed it. Its
--     primary key is dedupe_key, which serves the point lookup the sender does
--     but not a sweep bounded by time.
--
-- The retention registry now carries a policy for each. Both sweeps delete with
-- a single predicate — a timestamp column against a cutoff, and finished_at IS
-- NOT NULL — and no organization filter, so each needs an index that leads with
-- the timestamp column. The index is partial on finished_at IS NOT NULL because
-- only finished rows are ever eligible: an in-flight update or notification is
-- never swept, so it does not belong in the index either.
--
-- CONCURRENTLY is not used: migrations run inside a transaction, and CREATE INDEX
-- CONCURRENTLY cannot.

-- telegram_update_receipts: only a receipt whose reply has settled is swept.
CREATE INDEX IF NOT EXISTS telegram_update_receipts_finished_idx
    ON telegram_update_receipts (processed_at) WHERE finished_at IS NOT NULL;

-- telegram_task_delivery_receipts: only a receipt whose job has finished is swept.
CREATE INDEX IF NOT EXISTS telegram_task_delivery_receipts_finished_idx
    ON telegram_task_delivery_receipts (created_at) WHERE finished_at IS NOT NULL;
