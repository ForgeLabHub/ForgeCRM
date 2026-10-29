-- Reverses 0011.
--
-- Dropping the column loses only the record of when each session last stepped up.
-- Every session that survives becomes "has not stepped up", which is the safe
-- direction: a destructive decision from one of them is refused and the operator
-- steps up again.

ALTER TABLE sessions DROP COLUMN IF EXISTS step_up_at;
