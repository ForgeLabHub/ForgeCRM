ALTER TABLE import_jobs DROP COLUMN IF EXISTS config_hash;
ALTER TABLE migration_jobs DROP COLUMN IF EXISTS source_tables;
ALTER TABLE migration_jobs DROP COLUMN IF EXISTS secrets_encrypted;
