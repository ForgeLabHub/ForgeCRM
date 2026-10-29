-- Reverses 0007.
--
-- The unique index is dropped first: it is what prevents the duplicates the
-- dispatcher now relies on being rejected, and it cannot exist while rows are
-- being restored to their pre-0007 shape. The old partial index is recreated so a
-- rollback leaves the schema as 0006 left it.

DROP INDEX IF EXISTS webhook_deliveries_sub_event_uniq;

ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS locked_at;
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS locked_by;

DROP INDEX IF EXISTS webhook_deliveries_pending_idx;
CREATE INDEX webhook_deliveries_pending_idx ON webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'failed');

DROP INDEX IF EXISTS outbox_pending_idx;
CREATE INDEX outbox_pending_idx ON outbox_events (occurred_at) WHERE published_at IS NULL;

ALTER TABLE outbox_events DROP COLUMN IF EXISTS dead_lettered_at;
ALTER TABLE outbox_events DROP COLUMN IF EXISTS claimed_at;
ALTER TABLE outbox_events DROP COLUMN IF EXISTS claimed_by;
