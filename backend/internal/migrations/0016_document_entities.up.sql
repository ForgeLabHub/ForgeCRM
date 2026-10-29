-- A document may be attached to a ticket, a lead or a task, not only to an
-- account, a contact or a deal. The three columns are added rather than replaced,
-- so the existing attachments keep pointing at the same record.
--
-- ON DELETE SET NULL matches the existing columns: archiving the parent record
-- must not delete the file, and the document remains an organization-level
-- attachment.

ALTER TABLE documents
    ADD COLUMN ticket_id uuid REFERENCES tickets(id) ON DELETE SET NULL,
    ADD COLUMN lead_id   uuid REFERENCES leads(id)   ON DELETE SET NULL,
    ADD COLUMN task_id   uuid REFERENCES tasks(id)   ON DELETE SET NULL;

-- Each index supports "list the attachments of this record", which is the only
-- read pattern for the new columns, and a partial predicate keeps the index small
-- because most documents are attached to none of the three.
CREATE INDEX documents_ticket_idx ON documents (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE INDEX documents_lead_idx   ON documents (lead_id)   WHERE lead_id IS NOT NULL;
CREATE INDEX documents_task_idx   ON documents (task_id)   WHERE task_id IS NOT NULL;
