-- Reverses 0009.
--
-- Dropped in dependency order: approvals reference actions, which reference runs.
-- No data is preserved. A rollback that invented an approval for an action would be
-- worse than one that drops the record: a forged approval is precisely what the
-- signature in this schema exists to prevent, and a down-migration cannot produce a
-- valid one.

DROP TABLE IF EXISTS agent_approvals;
DROP TABLE IF EXISTS agent_actions;
DROP TABLE IF EXISTS agent_runs;
