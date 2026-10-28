-- 0010_seed_agent_permissions: seed the agent permission catalog entries and the
-- role grants that make the human-in-the-loop queue reachable.
--
-- The permissions were added to the Go catalog in the same change as this
-- migration. Without this migration the catalog would know the names but no role
-- could hold them, so only a platform owner — who bypasses the check — could
-- review or start an agent run. This is the same failure mode migration 0006
-- fixed for webhooks.manage.
--
-- Two permissions, deliberately distinct:
--
--   agent.read  view runs and the approval queue. This is the reviewer's right.
--   agent.run   start a run. This is the operator's right.
--
-- They are separate because reviewing what the agent proposes and setting it
-- running are different duties, usually held by different people. Collapsing them
-- would mean anyone who could decide an approval could also spawn the work that
-- needs approving, which defeats the separation the approval exists to create.

INSERT INTO permissions (key, category, description) VALUES
('agent.read', 'agent', 'View agent runs and the approval queue'),
('agent.run',  'agent', 'Start an agent run')
ON CONFLICT (key) DO NOTHING;

-- agent.read: everyone whose stated purpose includes oversight of the work the
-- agent does. The decision right (approvals.decide) is granted separately below,
-- so a role can see the queue without being able to act on it.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'agent.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'security_auditor',
                'executive', 'sales_manager', 'support_manager', 'readonly_auditor')
ON CONFLICT DO NOTHING;

-- agent.run: the roles that set the agent working. Restricted to the roles whose
-- purpose includes operating on data, not merely reading it.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'agent.run'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager')
ON CONFLICT DO NOTHING;

-- approvals.decide was catalogued by migration 0004 but never granted to a role,
-- which left the approval queue decidable only by a platform owner. Grant it to
-- the management roles whose purpose includes approving work, together with the
-- roles whose job is data governance.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'approvals.decide'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'data_steward',
                'sales_manager', 'support_manager')
ON CONFLICT DO NOTHING;

-- approvals.read for the oversight roles that should see requests without
-- deciding them, plus the auditor roles.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'approvals.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'security_auditor',
                'executive', 'data_steward', 'sales_manager', 'support_manager',
                'readonly_auditor')
ON CONFLICT DO NOTHING;
