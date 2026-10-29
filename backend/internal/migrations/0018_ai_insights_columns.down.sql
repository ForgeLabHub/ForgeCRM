-- Reverse 0018. `analysis` returns to jsonb; the two added columns are dropped.
-- The default is cleared before the type change because a `text` default of ''
-- cannot be cast to jsonb automatically.
ALTER TABLE ai_insights
    DROP COLUMN IF EXISTS draft,
    DROP COLUMN IF EXISTS recommendations,
    ALTER COLUMN analysis DROP DEFAULT,
    ALTER COLUMN analysis TYPE jsonb USING to_jsonb(analysis),
    ALTER COLUMN analysis SET DEFAULT '{}'::jsonb;

DROP INDEX IF EXISTS ai_insights_lookup_idx;
CREATE INDEX ai_insights_lookup_idx
    ON ai_insights (organization_id, entity_type, entity_id, kind, created_at DESC);
