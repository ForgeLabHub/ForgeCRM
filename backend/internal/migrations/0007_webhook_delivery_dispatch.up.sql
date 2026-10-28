-- 0007_webhook_delivery_dispatch: make webhook deliveries claimable, deduplicated
-- and bound.
--
-- Three defects are fixed here, all of which were invisible with a single worker
-- and a quiet queue:
--
--   1. Nothing ever sent a delivery. `webhook_deliveries` rows were created by the
--      dispatcher and then sat at 'pending' forever: no code path selected them.
--      A subscription could be created, a matching event could be committed, and
--      the endpoint would never be called. The worker now polls this table, and
--      that poll needs an index it can use.
--
--   2. A delivery could be sent twice. The send path loaded the row and posted it
--      without first claiming it, so two workers polling at the same moment both
--      saw 'pending' and both delivered. The claim is an UPDATE ... SKIP LOCKED,
--      which needs 'delivering' to be distinguishable from a stuck 'pending'.
--
--   3. Two workers could create two deliveries for the same event. The dispatcher
--      inserts one row per matching subscription, and two drains running
--      concurrently each inserted their own copy. The unique constraint below
--      makes that a duplicate-key error the dispatcher can absorb, instead of a
--      second HTTP POST to the customer's endpoint.

-- The claim index. The old index covered ('pending','failed'); a claimed delivery
-- is 'delivering', and a dead one is 'dead', so the partial predicate is widened.
-- Keeping it partial matters: the table grows with every delivered event and only
-- the small outstanding set is ever scanned.
DROP INDEX IF EXISTS webhook_deliveries_pending_idx;
CREATE INDEX webhook_deliveries_pending_idx ON webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'failed', 'delivering');

-- One delivery per (subscription, event). Both columns are NOT NULL in the
-- common case; outbox_event_id is nullable only because a delivery may in
-- principle be created without a backing outbox row, and PostgreSQL treats NULLs
-- as distinct in a unique index, so those rows never collide. A non-partial index
-- is used deliberately: ON CONFLICT can only infer a partial index when its
-- predicate is provably true for the statement, and the dispatcher's insert does
-- not guarantee that.
--
-- Deduplicate before adding the constraint: an existing deployment may already
-- hold duplicates created by the two defects above. Keeping the earliest row and
-- discarding the rest is safe because a duplicate that was never sent carries no
-- information the first row does not.
DELETE FROM webhook_deliveries a
USING webhook_deliveries b
WHERE a.subscription_id = b.subscription_id
  AND a.outbox_event_id = b.outbox_event_id
  AND a.outbox_event_id IS NOT NULL
  AND (a.created_at > b.created_at OR (a.created_at = b.created_at AND a.id > b.id));

CREATE UNIQUE INDEX webhook_deliveries_sub_event_uniq
    ON webhook_deliveries (subscription_id, outbox_event_id);

-- A sender claims a delivery before posting it, so two workers cannot both POST
-- to the customer's endpoint. 'delivering' plus a lock timestamp is what makes a
-- claim recoverable: a row stuck in 'delivering' past the timeout is reclaimed,
-- which is the only way an abandoned delivery is not lost.
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS locked_at timestamptz;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS locked_by text;

-- The outbox needs a bound too. An event that cannot be dispatched was retried
-- forever, so a permanently broken payload stayed in the pending set and was
-- re-read by every drain for the life of the deployment. `dead_lettered_at`
-- records the point at which a row stops being retried, so the pending set stays
-- bounded and an operator can see what was abandoned.
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS dead_lettered_at timestamptz;

-- A drain claims a row before dispatching it, so two workers cannot both pick up
-- the same event. The claim is released on success or failure, and reclaimed
-- after a timeout if the worker died holding it.
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS claimed_at timestamptz;
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS claimed_by text;

-- The pending scan is now "not published, not dead and not currently claimed";
-- a partial index keeps it small. The old index on (occurred_at) is superseded.
DROP INDEX IF EXISTS outbox_pending_idx;
CREATE INDEX outbox_pending_idx ON outbox_events (occurred_at)
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;
