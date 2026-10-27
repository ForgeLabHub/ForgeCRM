-- 0006_seed_webhooks_manage: seed the webhooks.manage permission.
--
-- The permission was added to the Go catalog and now guards six webhook
-- endpoints, but migration 0004 never inserted it. The effect was that no role
-- could hold it: only a platform owner (who bypasses the check) could create,
-- list, test or replay webhooks, and integration_admin could not.
--
-- This migration inserts the permission and grants it to the roles whose stated
-- purpose includes managing integrations and events. It is written to be safe on
-- a database where an operator has already added the row by hand.

INSERT INTO permissions (key, category, description)
VALUES ('webhooks.manage', 'events', 'Manage webhook subscriptions and deliveries')
ON CONFLICT (key) DO NOTHING;

-- Grant to the platform-wide roles. Platform role templates are the rows with no
-- organization; organizations clone them on creation, so this also covers
-- organizations created after this migration runs.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'webhooks.manage'
FROM roles r
WHERE r.organization_id IS NULL
  AND r.key IN ('platform_owner', 'platform_admin', 'integration_admin', 'tenant_admin')
ON CONFLICT DO NOTHING;

-- Grant to every organization's clone of the same roles, so existing tenants
-- gain the permission without waiting for a new organization to be provisioned.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'webhooks.manage'
FROM roles r
WHERE r.organization_id IS NOT NULL
  AND r.key IN ('platform_owner', 'platform_admin', 'integration_admin', 'tenant_admin')
ON CONFLICT DO NOTHING;
