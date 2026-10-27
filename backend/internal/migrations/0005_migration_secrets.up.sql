-- 0005_migration_secrets: store migration source credentials encrypted at rest.
--
-- The column is bytea holding AES-256-GCM ciphertext derived from the platform
-- secret. A plaintext password must never appear in this table or in any log.

ALTER TABLE migration_jobs
    ADD COLUMN secrets_encrypted bytea,
    ADD COLUMN source_tables jsonb NOT NULL DEFAULT '[]'::jsonb;

-- Import jobs also benefit from a recorded processing config hash so that a
-- re-run can be compared with the original.
ALTER TABLE import_jobs
    ADD COLUMN config_hash text;
