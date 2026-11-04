-- A rollback loses replay receipts and pending responses. Disable the webhook
-- first; do not roll back while Telegram may redeliver old updates.
DROP TABLE IF EXISTS telegram_update_receipts;
DROP INDEX IF EXISTS jobs_telegram_dedupe_idx;
DROP TABLE IF EXISTS telegram_task_delivery_receipts;
DROP TRIGGER IF EXISTS telegram_task_revision ON tasks;
DROP FUNCTION IF EXISTS telegram_update_task_revision();
ALTER TABLE tasks DROP COLUMN IF EXISTS telegram_due_revision;
