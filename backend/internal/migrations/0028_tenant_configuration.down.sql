DROP TABLE IF EXISTS tenant_setting_revisions;
DROP TABLE IF EXISTS tenant_settings;

DELETE FROM role_permissions WHERE permission_key IN
    ('tenant.settings.read', 'tenant.settings.manage');

DELETE FROM permissions WHERE key IN
    ('tenant.settings.read', 'tenant.settings.manage');
