-- 0018_ai_insights_columns: give the insight cache the columns the code writes.
--
-- 0014 created ai_insights before the contextual path was written, and the two
-- disagreed from the start. The code stores a model's analysis, its
-- recommendations and an optional draft; the table had a single `analysis`
-- column typed `jsonb`, and no `recommendations` or `draft` column at all. The
-- insert therefore named columns that do not exist and failed on every call.
--
-- That failure was swallowed on purpose — a cache write is best effort so a
-- failure to store does not fail a request the user already paid for — which is
-- exactly why it went unnoticed: the feature worked, the cache never did. Every
-- page view was a new billable model call, which is the cost 0014 exists to
-- avoid.
--
-- `analysis` is converted from jsonb to text rather than re-created, because the
-- code writes it a plain string. A jsonb value read back as a Go string arrives
-- JSON-quoted ("answer" with the quotes), so the conversion also unwraps any
-- existing value with `#>> '{}'`, which is identity for a JSON string and gives
-- the object's text for the `{}` default. Any row already stored was written by
-- a path that cannot have succeeded, so the table is in practice empty; the
-- USING clause is there so the migration is correct if it is not.

ALTER TABLE ai_insights
    ALTER COLUMN analysis TYPE text USING coalesce(analysis #>> '{}', ''),
    ALTER COLUMN analysis SET DEFAULT '',
    ADD COLUMN IF NOT EXISTS recommendations jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS draft jsonb;

-- The read path filters on the full key, including context_hash, so the lookup
-- index must lead with it; the unique constraint already orders these columns
-- this way, but the plain index the read uses did not include the hash.
DROP INDEX IF EXISTS ai_insights_lookup_idx;
CREATE INDEX ai_insights_lookup_idx
    ON ai_insights (organization_id, entity_type, entity_id, kind, context_hash);
