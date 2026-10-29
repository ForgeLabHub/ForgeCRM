-- 0013_seed_workflow_permissions: seed the workflow permission catalog entries
-- and the role grants that make automations usable.
--
-- Same shape as migrations 0006 and 0010: the permission names are added to the Go
-- catalog in the same change as this migration. Without the grants, the catalog
-- would know the names but no role could hold them, so only a platform owner —
-- who bypasses the check — could build or run a workflow.
--
-- Three permissions:
--
--   workflows.read    view workflows and their run history.
--   workflows.manage  create, edit, enable, disable and delete workflows.
--   workflows.run     start a run by hand and replay a failed one.
--
-- manage and run are distinct because a workflow executes as its author. The right
-- to define one is a modelling right; the right to trigger one is an execution
-- right, and on a regulated deployment the two are held by different people.

INSERT INTO permissions (key, category, description) VALUES
('workflows.read',   'workflows', 'View workflow definitions and run history'),
('workflows.manage', 'workflows', 'Create, edit, enable, disable and delete workflows'),
('workflows.run',    'workflows', 'Run a workflow by hand and replay a failed run')
ON CONFLICT (key) DO NOTHING;

-- workflows.read: every role whose purpose includes oversight of what the platform
-- does on its own. Read is broad because observing automation is a safe activity.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'workflows.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'security_auditor',
                'integration_admin', 'data_steward', 'executive', 'sales_manager',
                'support_manager', 'analyst', 'readonly_auditor')
ON CONFLICT DO NOTHING;

-- workflows.manage: the roles that model the business process. A workflow acts as
-- its author, so this is granted only to roles that already act on CRM data
-- directly; it does not by itself widen what data the author can reach.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'workflows.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager', 'integration_admin')
ON CONFLICT DO NOTHING;

-- workflows.run: the roles that may start a defined workflow on demand.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'workflows.run'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager', 'integration_admin')
ON CONFLICT DO NOTHING;
