-- 0004_rbac_seed: permission catalog and default role sets.
--
-- Roles are permission bundles stored as data. Nothing in the application should
-- branch on a role key; it checks permissions. An operator can create additional
-- roles and adjust these bundles from the admin panel.

INSERT INTO permissions (key, category, description) VALUES
-- CRM: contacts
('contacts.read',            'contacts',     'View contacts'),
('contacts.create',          'contacts',     'Create contacts'),
('contacts.update',          'contacts',     'Edit contacts'),
('contacts.archive',         'contacts',     'Archive contacts'),
('contacts.export',          'contacts',     'Export contacts'),
-- CRM: accounts
('accounts.read',            'accounts',     'View client organisations'),
('accounts.create',          'accounts',     'Create client organisations'),
('accounts.update',          'accounts',     'Edit client organisations'),
('accounts.archive',         'accounts',     'Archive client organisations'),
('accounts.export',          'accounts',     'Export client organisations'),
-- CRM: leads
('leads.read',               'leads',        'View leads'),
('leads.create',             'leads',        'Create leads'),
('leads.update',             'leads',        'Edit leads'),
('leads.archive',            'leads',        'Archive leads'),
('leads.convert',            'leads',        'Convert a lead into an account and contact'),
-- CRM: deals
('deals.read',               'deals',        'View deals'),
('deals.create',             'deals',        'Create deals'),
('deals.update',             'deals',        'Edit deals'),
('deals.archive',            'deals',        'Archive deals'),
('deals.export',             'deals',        'Export deals'),
('deals.approve_discount',   'deals',        'Approve a deal discount'),
('deals.change_stage',       'deals',        'Move a deal between pipeline stages'),
-- CRM: pipelines
('pipelines.read',           'pipelines',    'View pipelines and stages'),
('pipelines.manage',         'pipelines',    'Create and edit pipelines and stages'),
-- CRM: tasks and activities
('tasks.read',               'tasks',        'View tasks'),
('tasks.create',             'tasks',        'Create tasks'),
('tasks.update',             'tasks',        'Edit tasks'),
('tasks.assign',             'tasks',        'Assign tasks to other users'),
('activities.read',          'activities',   'View activities and history'),
('activities.create',        'activities',   'Record activities'),
-- CRM: tickets
('tickets.read',             'tickets',      'View support tickets'),
('tickets.create',           'tickets',      'Create support tickets'),
('tickets.update',           'tickets',      'Edit support tickets'),
('tickets.assign',           'tickets',      'Assign support tickets'),
('tickets.close',            'tickets',      'Close support tickets'),
('sla.manage',               'tickets',      'Manage SLA policies'),
-- CRM: documents, tags, custom fields
('documents.read',           'documents',    'View documents'),
('documents.create',         'documents',    'Upload documents'),
('documents.archive',        'documents',    'Archive documents'),
('tags.manage',              'tags',         'Manage tags'),
('custom_fields.manage',     'custom_fields','Manage custom field definitions'),
('comments.create',          'comments',     'Add comments'),
-- Platform: users, roles, orgs, teams
('users.read',               'users',        'View users'),
('users.manage',             'users',        'Invite, block and edit users'),
('roles.read',               'roles',        'View roles and permissions'),
('roles.manage',             'roles',        'Create roles and change permission sets'),
('organizations.read',       'organizations','View organizations'),
('organizations.manage',     'organizations','Create and edit organizations'),
('teams.manage',             'teams',        'Manage teams and membership'),
-- Data
('imports.read',             'imports',      'View import jobs and reports'),
('imports.create',           'imports',      'Create import jobs'),
('imports.execute',          'imports',      'Run an import'),
('imports.approve',          'imports',      'Approve an import before execution'),
('exports.read',             'exports',      'View exports'),
('exports.execute',          'exports',      'Run an export'),
('migrations.read',          'migrations',   'View migration jobs and reconciliation'),
('migrations.manage',        'migrations',   'Create and run migrations from external systems'),
('data.manage_quality',      'data',         'Manage data quality and deduplication'),
-- Integrations and events
('integrations.read',        'integrations', 'View integrations and runs'),
('integrations.configure',   'integrations', 'Create, configure and revoke integrations'),
('events.read',              'events',       'View the event log'),
('events.manage',            'events',       'Manage webhook subscriptions'),
-- Observability and security
('audit.read',               'audit',        'Read the audit trail'),
('security.read',            'security',     'Read authentication and security events'),
('security.configure',       'security',     'Change security settings'),
('logs.read',                'observability','Read application logs'),
('metrics.read',             'observability','Read metrics and diagnostics'),
('jobs.read',                'jobs',         'View background jobs'),
('jobs.manage',              'jobs',         'Cancel and retry background jobs'),
-- Reporting
('reports.read',             'reports',      'View reports'),
('reports.export',           'reports',      'Export reports'),
-- Notifications
('notifications.read',       'notifications','View own notifications'),
('email.manage',             'notifications','Configure and test email delivery'),
('telegram.manage',          'notifications','Configure the Telegram bot and links'),
-- AI
('ai.read',                  'ai',           'View AI status and tasks'),
('ai.tools.execute',         'ai',           'Invoke AI tools and MCP endpoints'),
('ai.configure',             'ai',           'Configure AI providers'),
-- Approvals and system
('approvals.read',           'approvals',    'View approval requests'),
('approvals.decide',         'approvals',    'Approve or reject requests'),
('system.settings',          'system',       'Change platform settings')
ON CONFLICT (key) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Default roles. organization_id is NULL: these are platform templates cloned
-- into an organization when it is created.
-- ---------------------------------------------------------------------------

INSERT INTO roles (key, name, description, is_system) VALUES
('platform_owner',       'Platform owner',       'Full control of the platform instance', true),
('platform_admin',       'Platform administrator','Administers the platform instance', true),
('tenant_admin',         'Organization administrator','Administers a single organization', true),
('security_auditor',     'Security auditor',     'Reads security and audit logs', true),
('integration_admin',    'Integration administrator','Manages integration connections', true),
('data_steward',         'Data steward',         'Manages data quality and migration', true),
('executive',            'Executive',            'Management reporting', true),
('sales_manager',        'Sales manager',        'Manages sales and the sales team', true),
('sales_representative', 'Sales representative', 'Works with own clients and deals', true),
('support_manager',      'Support manager',      'Manages the support desk', true),
('support_agent',        'Support agent',        'Handles assigned tickets', true),
('analyst',              'Analyst',              'Permitted analytics', true),
('employee',             'Employee',             'General permitted work operations', true),
('readonly_auditor',     'Read-only auditor',    'Read-only access to permitted records', true)
ON CONFLICT DO NOTHING;

-- Helper: grant a list of permissions to a role.
CREATE OR REPLACE FUNCTION seed_grant(role_key text, perms text[]) RETURNS void AS $$
DECLARE
    rid uuid;
BEGIN
    SELECT id INTO rid FROM roles WHERE key = role_key AND organization_id IS NULL;
    IF rid IS NULL THEN
        RAISE EXCEPTION 'seed role % not found', role_key;
    END IF;
    INSERT INTO role_permissions (role_id, permission_key)
    SELECT rid, p FROM unnest(perms) AS p
    ON CONFLICT DO NOTHING;
END;
$$ LANGUAGE plpgsql;

-- platform_owner: every permission.
SELECT seed_grant('platform_owner', ARRAY(SELECT key FROM permissions));

-- platform_admin: everything except the platform-owner-only destructive knobs.
SELECT seed_grant('platform_admin', ARRAY(
    SELECT key FROM permissions
    WHERE key NOT IN ('organizations.manage')
));

-- tenant_admin: full control inside one organization.
SELECT seed_grant('tenant_admin', ARRAY(
    SELECT key FROM permissions
    WHERE category NOT IN ('ai')
      AND key NOT IN ('system.settings', 'security.configure', 'organizations.manage',
                      'migrations.manage', 'platform.owner')
      AND key <> 'logs.read'
));

-- security_auditor
SELECT seed_grant('security_auditor', ARRAY[
    'audit.read', 'security.read', 'users.read', 'roles.read', 'organizations.read',
    'logs.read', 'metrics.read', 'events.read', 'integrations.read', 'jobs.read'
]);

-- integration_admin
SELECT seed_grant('integration_admin', ARRAY[
    'integrations.read', 'integrations.configure', 'events.read', 'events.manage',
    'jobs.read', 'jobs.manage', 'imports.read', 'migrations.read', 'migrations.manage',
    'logs.read', 'audit.read', 'organizations.read'
]);

-- data_steward
SELECT seed_grant('data_steward', ARRAY[
    'imports.read', 'imports.create', 'imports.execute', 'imports.approve',
    'exports.read', 'exports.execute', 'migrations.read', 'migrations.manage',
    'data.manage_quality', 'accounts.read', 'accounts.update', 'contacts.read',
    'contacts.update', 'leads.read', 'leads.update', 'deals.read', 'deals.update',
    'custom_fields.manage', 'tags.manage', 'audit.read', 'jobs.read'
]);

-- executive
SELECT seed_grant('executive', ARRAY[
    'accounts.read', 'contacts.read', 'leads.read', 'deals.read', 'pipelines.read',
    'tasks.read', 'activities.read', 'tickets.read', 'documents.read',
    'reports.read', 'reports.export', 'approvals.read', 'notifications.read',
    'integrations.read', 'ai.read'
]);

-- sales_manager
SELECT seed_grant('sales_manager', ARRAY[
    'accounts.read', 'accounts.create', 'accounts.update', 'accounts.archive', 'accounts.export',
    'contacts.read', 'contacts.create', 'contacts.update', 'contacts.archive', 'contacts.export',
    'leads.read', 'leads.create', 'leads.update', 'leads.archive', 'leads.convert',
    'deals.read', 'deals.create', 'deals.update', 'deals.archive', 'deals.export',
    'deals.approve_discount', 'deals.change_stage',
    'pipelines.read', 'pipelines.manage',
    'tasks.read', 'tasks.create', 'tasks.update', 'tasks.assign',
    'activities.read', 'activities.create',
    'tickets.read', 'documents.read', 'documents.create',
    'reports.read', 'reports.export', 'notifications.read', 'users.read', 'teams.manage',
    'comments.create', 'tags.manage', 'ai.read', 'ai.tools.execute'
]);

-- sales_representative
SELECT seed_grant('sales_representative', ARRAY[
    'accounts.read', 'accounts.create', 'accounts.update',
    'contacts.read', 'contacts.create', 'contacts.update',
    'leads.read', 'leads.create', 'leads.update', 'leads.convert',
    'deals.read', 'deals.create', 'deals.update', 'deals.change_stage',
    'pipelines.read', 'tasks.read', 'tasks.create', 'tasks.update',
    'activities.read', 'activities.create', 'tickets.read', 'tickets.create',
    'documents.read', 'documents.create', 'reports.read', 'notifications.read',
    'comments.create', 'ai.read', 'ai.tools.execute'
]);

-- support_manager
SELECT seed_grant('support_manager', ARRAY[
    'tickets.read', 'tickets.create', 'tickets.update', 'tickets.assign', 'tickets.close',
    'sla.manage', 'accounts.read', 'contacts.read', 'contacts.update',
    'activities.read', 'activities.create', 'tasks.read', 'tasks.create', 'tasks.assign',
    'documents.read', 'documents.create', 'reports.read', 'reports.export',
    'notifications.read', 'users.read', 'comments.create', 'ai.read', 'ai.tools.execute'
]);

-- support_agent
SELECT seed_grant('support_agent', ARRAY[
    'tickets.read', 'tickets.create', 'tickets.update', 'tickets.close',
    'accounts.read', 'contacts.read', 'activities.read', 'activities.create',
    'tasks.read', 'tasks.create', 'tasks.update', 'documents.read',
    'notifications.read', 'comments.create', 'ai.read', 'ai.tools.execute'
]);

-- analyst
SELECT seed_grant('analyst', ARRAY[
    'accounts.read', 'contacts.read', 'leads.read', 'deals.read', 'pipelines.read',
    'tasks.read', 'activities.read', 'tickets.read', 'documents.read',
    'reports.read', 'reports.export', 'notifications.read', 'imports.read', 'ai.read'
]);

-- employee
SELECT seed_grant('employee', ARRAY[
    'accounts.read', 'contacts.read', 'leads.read', 'deals.read', 'pipelines.read',
    'tasks.read', 'tasks.create', 'tasks.update', 'activities.read', 'activities.create',
    'tickets.read', 'tickets.create', 'documents.read', 'notifications.read', 'comments.create'
]);

-- readonly_auditor
SELECT seed_grant('readonly_auditor', ARRAY[
    'accounts.read', 'contacts.read', 'leads.read', 'deals.read', 'pipelines.read',
    'tasks.read', 'activities.read', 'tickets.read', 'documents.read',
    'audit.read', 'reports.read', 'notifications.read'
]);

DROP FUNCTION seed_grant(text, text[]);
