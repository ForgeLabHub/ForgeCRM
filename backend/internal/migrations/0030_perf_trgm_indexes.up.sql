-- Trigram indexes for the measured ILIKE search paths.
--
-- Every filtered list and the cross-entity search match with ILIKE '%term%'. A
-- leading wildcard cannot use a B-tree index, so each of these predicates was a
-- sequential scan that read the whole table and discarded most of it: at 10k
-- contacts the list filter read 8,888 of 10,000 rows to return 1,112, and the
-- cross-entity search ran six of those scans per request. pg_trgm decomposes the
-- pattern into trigrams and a GIN index matches on them, turning the predicate
-- into a bitmap index scan. Measured at 200k contacts, the contact list filter
-- goes from 124 ms (parallel seq scan) to 14.7 ms once the indexes below are in
-- place.
--
-- Only the expressions the queries use are indexed, and each index is here
-- because a measured plan showed the sequential scan it replaces. The
-- expressions are copied verbatim from the query text: an expression index is
-- only usable when the planner sees the identical expression, so `name` and
-- `COALESCE(name,'')` are different indexes, and a cast is part of the
-- expression.
--
-- Email columns are citext, and a trigram GIN index on a citext column is never
-- chosen by the planner: citext's ILIKE is a different operator on a different
-- operator class, and forcing the plan still reads the whole table. The indexes
-- are therefore on `email::text`, and the list queries cast to text to match —
-- which is what the cross-entity search path already did. The two forms agree on
-- case-insensitive matching; that is the whole point of citext, and the search
-- path already depends on it.
--
-- Reversible: 0030_perf_trgm_indexes.down.sql drops the extension and every
-- index below.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Contacts list filter and contact search: first_name / last_name / email.
CREATE INDEX contacts_first_name_trgm_idx ON contacts USING gin (COALESCE(first_name, '') gin_trgm_ops);
CREATE INDEX contacts_last_name_trgm_idx  ON contacts USING gin (COALESCE(last_name, '') gin_trgm_ops);
CREATE INDEX contacts_email_trgm_idx      ON contacts USING gin ((COALESCE(email::text, '')) gin_trgm_ops);

-- Accounts list filter and account search: name / email.
CREATE INDEX accounts_name_trgm_idx       ON accounts USING gin (name gin_trgm_ops);
CREATE INDEX accounts_email_trgm_idx      ON accounts USING gin ((COALESCE(email::text, '')) gin_trgm_ops);

-- Leads list filter and lead search: title / company / email.
CREATE INDEX leads_title_trgm_idx         ON leads USING gin (title gin_trgm_ops);
CREATE INDEX leads_company_trgm_idx       ON leads USING gin (COALESCE(company, '') gin_trgm_ops);
CREATE INDEX leads_email_trgm_idx         ON leads USING gin ((COALESCE(email::text, '')) gin_trgm_ops);

-- Single-column search paths.
CREATE INDEX deals_title_trgm_idx         ON deals USING gin (title gin_trgm_ops);
CREATE INDEX tickets_subject_trgm_idx     ON tickets USING gin (subject gin_trgm_ops);
CREATE INDEX tasks_title_trgm_idx         ON tasks USING gin (title gin_trgm_ops);

-- Activities search: subject / body.
CREATE INDEX activities_subject_trgm_idx  ON activities USING gin (subject gin_trgm_ops);
CREATE INDEX activities_body_trgm_idx     ON activities USING gin (COALESCE(body, '') gin_trgm_ops);

-- Admin user list (email / display_name) and admin log search (message).
CREATE INDEX users_email_trgm_idx         ON users USING gin ((email::text) gin_trgm_ops);
CREATE INDEX users_display_name_trgm_idx  ON users USING gin (display_name gin_trgm_ops);
CREATE INDEX app_logs_message_trgm_idx    ON app_logs USING gin (message gin_trgm_ops);
