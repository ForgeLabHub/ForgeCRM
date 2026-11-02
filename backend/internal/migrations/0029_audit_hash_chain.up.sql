-- Tamper-evident audit trail.
--
-- The audit log is already append-only (a trigger refuses UPDATE and DELETE), but
-- append-only only proves the database refused an edit through the API. An actor
-- with direct write access to the table could still delete or rewrite a row. A
-- hash chain makes that visible: every entry carries the hash of the entry before
-- it in its organization's chain, so removing or altering one row breaks every
-- hash that follows and the verifier reports the first break.
--
-- The columns are nullable on purpose. Rows written before this migration have no
-- chain, and they cannot be retroactively chained without rewriting them, which
-- the append-only trigger forbids. A NULL entry_hash therefore means "written
-- before the chain existed" and the verifier skips such rows; a row written after
-- this migration always has one.
--
-- The chain is per organization. Tenant isolation is a property of this product,
-- so one tenant's trail must be verifiable on its own, without reading another
-- tenant's rows. Platform-level entries (organization_id IS NULL) form their own
-- chain.

ALTER TABLE audit_log ADD COLUMN prev_hash  bytea;
ALTER TABLE audit_log ADD COLUMN entry_hash bytea;

-- The tail lookup ("the last chained entry for this organization") is on the
-- append path, so it runs on every audited write and needs an index. Ordering is
-- by id, which is monotonic, so the index carries it.
CREATE INDEX audit_log_chain_tail_idx
    ON audit_log (organization_id, id DESC)
    WHERE entry_hash IS NOT NULL;
