-- Reverses 0006: remove the webhooks.manage grants and the permission itself.
--
-- The role grants must go first: role_permissions.permission_key references
-- permissions.key, so deleting the permission while grants remain would fail.

DELETE FROM role_permissions WHERE permission_key = 'webhooks.manage';
DELETE FROM permissions WHERE key = 'webhooks.manage';
