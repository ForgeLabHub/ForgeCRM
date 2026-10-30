-- Reverse 0022. The permission grants, the catalog entries and the tables are
-- removed in dependency order.
DROP TABLE IF EXISTS oidc_login_states;
DROP TABLE IF EXISTS user_identities;
DROP TABLE IF EXISTS oidc_providers;

DELETE FROM role_permissions WHERE permission_key IN ('sso.read', 'sso.manage');
DELETE FROM permissions WHERE key IN ('sso.read', 'sso.manage');
