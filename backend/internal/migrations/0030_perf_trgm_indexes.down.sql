-- Reverses 0030. Every index is dropped, then the extension.
--
-- The extension is dropped because 0030 created it and nothing before it needed
-- it; leaving it installed would make the down migration non-reversible in the
-- other direction (a later 0030 would find it already present). Dropping it
-- cascades to any object that still depends on it, which is why the indexes go
-- first and explicitly.

DROP INDEX IF EXISTS app_logs_message_trgm_idx;
DROP INDEX IF EXISTS users_display_name_trgm_idx;
DROP INDEX IF EXISTS users_email_trgm_idx;
DROP INDEX IF EXISTS activities_body_trgm_idx;
DROP INDEX IF EXISTS activities_subject_trgm_idx;
DROP INDEX IF EXISTS tasks_title_trgm_idx;
DROP INDEX IF EXISTS tickets_subject_trgm_idx;
DROP INDEX IF EXISTS deals_title_trgm_idx;
DROP INDEX IF EXISTS leads_email_trgm_idx;
DROP INDEX IF EXISTS leads_company_trgm_idx;
DROP INDEX IF EXISTS leads_title_trgm_idx;
DROP INDEX IF EXISTS accounts_email_trgm_idx;
DROP INDEX IF EXISTS accounts_name_trgm_idx;
DROP INDEX IF EXISTS contacts_email_trgm_idx;
DROP INDEX IF EXISTS contacts_last_name_trgm_idx;
DROP INDEX IF EXISTS contacts_first_name_trgm_idx;

DROP EXTENSION IF EXISTS pg_trgm;
