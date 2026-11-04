-- Idempotency receipts for Telegram webhook updates. Responses are encrypted
-- until the Telegram API accepts them; no incoming body or link code is saved.
CREATE TABLE telegram_update_receipts (
    update_id bigint PRIMARY KEY CHECK (update_id >= 0),
    reply_ciphertext bytea,
    processed_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    outcome text CHECK (outcome IN ('accepted', 'blocked', 'unavailable'))
);
CREATE INDEX telegram_updates_pending_age_idx ON telegram_update_receipts (processed_at) WHERE reply_ciphertext IS NOT NULL;

-- The existing jobs queue is the delivery queue; event replays cannot enqueue
-- the same notification or the same task/assignee/due overdue reminder twice.
CREATE UNIQUE INDEX jobs_telegram_dedupe_idx ON jobs ((payload->>'dedupe_key'))
    WHERE kind = 'telegram.task_notification';

-- This revision identifies one overdue reminder per due/assignee schedule.
-- Reopening a task or returning to a previous deadline creates a new schedule.
ALTER TABLE tasks ADD COLUMN telegram_due_revision uuid NOT NULL DEFAULT gen_random_uuid();
CREATE FUNCTION telegram_update_task_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.assignee_user_id IS DISTINCT FROM OLD.assignee_user_id OR NEW.due_at IS DISTINCT FROM OLD.due_at
       OR (OLD.status NOT IN ('open', 'in_progress') AND NEW.status IN ('open', 'in_progress')) THEN
        NEW.telegram_due_revision := gen_random_uuid();
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER telegram_task_revision BEFORE UPDATE OF assignee_user_id, due_at, status ON tasks
    FOR EACH ROW EXECUTE FUNCTION telegram_update_task_revision();

-- Durable idempotency ledger, not a second queue. Jobs remain the sole queue;
-- this receipt survives normal retention of completed job records.
CREATE TABLE telegram_task_delivery_receipts (
    dedupe_key text PRIMARY KEY,
    job_id uuid,
    outcome text,
    image_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
