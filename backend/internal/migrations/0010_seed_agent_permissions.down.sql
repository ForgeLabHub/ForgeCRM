-- Reverses 0010.
--
-- Only the agent permission entries and their grants are removed. The grants go
-- first, because role_permissions.permission_key references permissions.key and
-- deleting the permission while grants remain would fail.
--
-- The approvals.read and approvals.decide grants this migration added are left in
-- place deliberately. They are additive grants of permissions that migration 0004
-- already catalogued, so removing them here would revert a grant 0004 owns.
-- Leaving them makes the rollback a strict subset of the forward change, which is
-- what a down migration should be.

DELETE FROM role_permissions WHERE permission_key IN ('agent.read', 'agent.run');
DELETE FROM permissions WHERE key IN ('agent.read', 'agent.run');
