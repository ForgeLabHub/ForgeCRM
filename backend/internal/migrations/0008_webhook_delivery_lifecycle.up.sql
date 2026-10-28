-- 0008_webhook_delivery_lifecycle: record when a delivery enters the terminal
-- dead state, and count operator replays without discarding the attempt history.
--
-- Two defects are addressed:
--
--   1. A delivery that exhausted its attempts became 'dead' and nothing else
--      happened. There was no audit entry, so an operator reading the audit trail
--      could not see that a customer's endpoint had stopped receiving events, and
--      no counter existed to alert on. `dead_lettered_at` marks the transition so
--      the audit entry and the metric are derived from one authoritative moment
--      rather than recomputed from attempts, which a replay changes.
--
--   2. Replay reset `attempts` to zero and left no trace, so an operator could not
--      tell a delivery that had just died from one that had already exhausted two
--      full retry budgets. `attempts` is still the per-window count the sender
--      compares against max_attempts, so resetting it is what grants a replayed
--      delivery its fresh budget; `replay_count` is the lifetime number of windows,
--      never reset, so the history the operator needs survives a replay.

ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS dead_lettered_at timestamptz;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS replay_count integer NOT NULL DEFAULT 0;

-- Rows that are already dead predate the column. Marking them now keeps the
-- metric and the audit-visible state consistent for an existing deployment
-- instead of reporting a dead delivery as if it were still in flight.
UPDATE webhook_deliveries
SET dead_lettered_at = COALESCE(delivered_at, next_attempt_at, created_at)
WHERE status = 'dead' AND dead_lettered_at IS NULL;

-- Partial index for the "how many are dead" reads. Dead rows are a small subset
-- of a table that grows with every delivered event.
CREATE INDEX IF NOT EXISTS webhook_deliveries_dead_idx
    ON webhook_deliveries (dead_lettered_at)
    WHERE dead_lettered_at IS NOT NULL;
