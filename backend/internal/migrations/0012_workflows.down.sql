-- Reverses 0012: drop the workflow engine tables.
--
-- Order matters only for the foreign keys, and every child references its parent
-- with ON DELETE CASCADE, so dropping the parents would take the children with
-- them. They are dropped explicitly anyway, in dependency order, so the intent is
-- readable and a future schema change cannot quietly leave one behind.

DROP TABLE IF EXISTS workflow_run_events;
DROP TABLE IF EXISTS workflow_step_runs;
DROP TABLE IF EXISTS workflow_runs;
DROP TABLE IF EXISTS workflows;
