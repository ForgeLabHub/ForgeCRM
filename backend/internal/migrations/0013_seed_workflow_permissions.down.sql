-- Reverses 0013: remove the workflow grants and the permissions themselves.
--
-- The grants go first: role_permissions.permission_key references permissions.key,
-- so deleting a permission while a grant still points at it would fail.

DELETE FROM role_permissions WHERE permission_key IN ('workflows.read', 'workflows.manage', 'workflows.run');
DELETE FROM permissions WHERE key IN ('workflows.read', 'workflows.manage', 'workflows.run');
