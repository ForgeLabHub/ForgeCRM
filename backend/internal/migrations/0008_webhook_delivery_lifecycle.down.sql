-- Reverses 0008.
--
-- The dead-letter index is dropped with the column it covers, and replay_count is
-- removed. No data is restored: the previous schema had nowhere to keep it, and a
-- rollback that invented a value would be worse than one that drops it.

DROP INDEX IF EXISTS webhook_deliveries_dead_idx;
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS replay_count;
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS dead_lettered_at;
