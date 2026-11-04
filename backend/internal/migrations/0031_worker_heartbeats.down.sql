-- Reverses 0031. Dropping the table discards the liveness record, which is the
-- only way to undo the migration: the rows are timestamps, not derived data.
DROP TABLE IF EXISTS worker_heartbeats;
