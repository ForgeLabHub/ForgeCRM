-- Reverse 0027. The grants, the catalog entries, the indexes and the table are
-- removed in dependency order. The self-referencing column is dropped with the
-- table, so there is no separate constraint to unwind.
DROP TABLE IF EXISTS consent_records;

DELETE FROM role_permissions WHERE permission_key IN ('consent.read', 'consent.manage');
DELETE FROM permissions WHERE key IN ('consent.read', 'consent.manage');
