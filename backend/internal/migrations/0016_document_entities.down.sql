DROP INDEX IF EXISTS documents_task_idx;
DROP INDEX IF EXISTS documents_lead_idx;
DROP INDEX IF EXISTS documents_ticket_idx;

ALTER TABLE documents
    DROP COLUMN IF EXISTS task_id,
    DROP COLUMN IF EXISTS lead_id,
    DROP COLUMN IF EXISTS ticket_id;
