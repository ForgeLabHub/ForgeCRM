-- Reverses 0029. Dropping the columns discards the chain, which is the only way
-- to undo the migration without rewriting rows the append-only trigger protects.
DROP INDEX IF EXISTS audit_log_chain_tail_idx;
ALTER TABLE audit_log DROP COLUMN IF EXISTS entry_hash;
ALTER TABLE audit_log DROP COLUMN IF EXISTS prev_hash;
