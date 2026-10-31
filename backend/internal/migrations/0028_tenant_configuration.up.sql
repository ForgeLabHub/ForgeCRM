-- 0028_tenant_configuration: a place for an organization's own settings.
--
-- Until now the only settings store was `app_settings`, a flat key/value table
-- with no organization column. Everything in it is deployment-wide, and the
-- handler refuses a key that looks like a credential, which is right for a table
-- every administrator can read. Nothing let one organization choose a default
-- that applies only to itself, so a platform serving several tenants had to pick
-- one value for all of them — the default currency of a deal, the timezone a
-- scheduled workflow runs in, the name an outbound email is sent under.
--
-- The model is two tables rather than one.
--
-- `tenant_settings` holds the current override for a key. A key with no row is
-- not missing: it resolves to the default the catalog declares, which is why the
-- platform needs no seed step and why materialising every default as a row would
-- be worse — it would put the default in two places, and the two would drift.
--
-- `tenant_setting_revisions` is the history. A change writes a new revision and
-- leaves the previous ones alone, so "who changed the currency, when, and to
-- what" is answerable after the fact. The audit trail records the same event, but
-- a revision chain is what an operator reads when they are asked to explain a
-- value, and it survives even when the audit retention window has closed.
--
-- Every row carries `schema_version`. The catalog is versioned in code, and a
-- value written under one schema may mean something else under the next; storing
-- the version with the value is what makes an old row interpretable rather than
-- guessed at.

CREATE TABLE tenant_settings (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- The catalog key, e.g. 'crm.default_currency'. The set of keys is closed in
    -- the application: a free-text key would be a setting nothing reads.
    key             text NOT NULL CHECK (length(btrim(key)) BETWEEN 1 AND 120),
    -- The override, as JSON. The shape is the catalog's business, not this table's:
    -- one column for every kind keeps the schema stable when a kind is added.
    value           jsonb NOT NULL,
    -- The revision this value was written at. It is the value in the history chain
    -- that this row is the head of.
    revision        integer NOT NULL DEFAULT 1 CHECK (revision >= 1),
    -- The catalog version the value was validated against.
    schema_version  integer NOT NULL CHECK (schema_version >= 1),
    updated_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, key)
);

CREATE TRIGGER tenant_settings_updated_at BEFORE UPDATE ON tenant_settings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE tenant_setting_revisions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    key              text NOT NULL,
    revision         integer NOT NULL CHECK (revision >= 1),
    -- The value that became current at this revision. NULL when the revision is a
    -- reset, because "back to the default" has no stored value to record; the
    -- flag below says so explicitly rather than leaving NULL ambiguous.
    value            jsonb,
    reset_to_default boolean NOT NULL DEFAULT false,
    schema_version   integer NOT NULL CHECK (schema_version >= 1),
    -- The value that was current immediately before this revision, so a diff can be
    -- read from one row without walking the chain backwards.
    previous_value   jsonb,
    changed_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- One row per revision of one key. A concurrent writer that tried to reuse a
    -- revision number would collide here rather than silently overwrite history.
    UNIQUE (organization_id, key, revision),
    CONSTRAINT tenant_setting_revisions_reset_has_no_value
        CHECK (NOT reset_to_default OR value IS NULL)
);

-- The read the API performs: one key's history, newest first.
CREATE INDEX tenant_setting_revisions_key_idx
    ON tenant_setting_revisions (organization_id, key, revision DESC);

-- The listing an operator works through across all keys.
CREATE INDEX tenant_setting_revisions_listing_idx
    ON tenant_setting_revisions (organization_id, created_at DESC);

-- A retention sweep deletes by created_at across organizations, so it needs an
-- index that leads with that column; the listing index leads with organization_id.
CREATE INDEX tenant_setting_revisions_retention_idx ON tenant_setting_revisions (created_at);

-- Permission catalog entries. Same shape as migrations 0006, 0013, 0019, 0020 and
-- 0027: the names are added to the Go catalog (internal/rbac) in the same change
-- as this migration, and TestSeedDataMatchesThePermissionCatalog fails if the two
-- disagree.
--
--   tenant.settings.read    view the organization's effective configuration.
--   tenant.settings.manage  change it.
--
-- Reading is separate from changing because the values steer defaults the whole
-- organization inherits: an auditor needs to see which currency the numbers are
-- in without being able to move it.
INSERT INTO permissions (key, category, description) VALUES
('tenant.settings.read',   'tenant', 'View the organization configuration'),
('tenant.settings.manage', 'tenant', 'Change the organization configuration')
ON CONFLICT (key) DO NOTHING;

-- tenant.settings.read: roles whose work depends on the organization's defaults.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'tenant.settings.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'security_auditor',
                'readonly_auditor', 'executive', 'sales_manager', 'support_manager',
                'analyst', 'data_steward', 'integration_admin')
ON CONFLICT DO NOTHING;

-- tenant.settings.manage: changing a shared default affects every user in the
-- organization, so it is narrower than reading it.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'tenant.settings.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin')
ON CONFLICT DO NOTHING;
