-- 0019_simulation_lab: saved scenarios, their runs, and the permission catalog
-- entries that guard them.
--
-- Two tables. A scenario is the user's saved definition: which kind of "what if"
-- and the parameters. A run is one computation of a scenario against a frozen
-- baseline. Runs are immutable: a stored run keeps the exact parameters and
-- baseline it was computed from, plus the algorithm version, so a result computed
-- months ago stays interpretable after the maths changes. Recomputing the same
-- inputs under the same version yields the same numbers, which is why the result
-- is stored as data rather than recomputed on read.
--
-- Money in a result is stored as integer minor units, never a float, so a
-- reloaded result cannot drift. The result column is jsonb for that reason; the
-- shape is owned by internal/analytics, which is the only writer.
--
-- Nothing here can modify CRM data. The tables hold the user's own modelling
-- inputs and outputs; the baseline they are computed against is read, not
-- written.

CREATE TABLE simulation_scenarios (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    owner_user_id     uuid REFERENCES users(id) ON DELETE SET NULL,
    name              text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    description       text,
    kind              text NOT NULL CHECK (kind IN ('funnel', 'revenue', 'retention')),
    -- params holds the scenario parameters as jsonb. The exact shape is owned by
    -- internal/analytics; storing it as a document keeps a parameter-set change
    -- from needing a schema migration, and the algorithm version makes an old
    -- document interpretable.
    params            jsonb NOT NULL DEFAULT '{}'::jsonb,
    algorithm_version text NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    archived_at       timestamptz
);

CREATE TRIGGER simulation_scenarios_updated_at BEFORE UPDATE ON simulation_scenarios
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A scenario name is unique per organization among live rows, so two people do
-- not create "Q3 best case" twice by accident. A partial unique index lets an
-- archived scenario keep its name, so re-creating one is possible after deleting
-- the first.
CREATE UNIQUE INDEX simulation_scenarios_name_uniq
    ON simulation_scenarios (organization_id, lower(name))
    WHERE archived_at IS NULL;

-- The list is "the scenarios I can see in my organization", most recent first.
CREATE INDEX simulation_scenarios_org_idx
    ON simulation_scenarios (organization_id, created_at DESC)
    WHERE archived_at IS NULL;

CREATE TABLE simulation_runs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    scenario_id       uuid REFERENCES simulation_scenarios(id) ON DELETE CASCADE,
    created_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    kind              text NOT NULL CHECK (kind IN ('funnel', 'revenue', 'retention')),
    algorithm_version text NOT NULL,
    -- params and baseline are frozen copies: the parameters as supplied and the
    -- observed data the run was computed against. Without the frozen baseline a
    -- stored run could not be explained after the CRM data moved on.
    params            jsonb NOT NULL DEFAULT '{}'::jsonb,
    baseline          jsonb NOT NULL DEFAULT '{}'::jsonb,
    result            jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- Run history for a scenario, newest first.
CREATE INDEX simulation_runs_scenario_idx
    ON simulation_runs (organization_id, scenario_id, created_at DESC);

-- The organization's recent runs, for the activity view.
CREATE INDEX simulation_runs_org_idx
    ON simulation_runs (organization_id, created_at DESC);

-- Permission catalog entries. Same shape as migrations 0006, 0010 and 0013: the
-- names are added to the Go catalog (internal/rbac) in the same change as this
-- migration, and TestSeedDataMatchesThePermissionCatalog fails if the two ever
-- disagree.
--
--   simulation.read    view saved scenarios and run history.
--   simulation.manage  create, edit and delete a saved scenario.
--   simulation.run     compute a scenario (including an unsaved preview).
--
-- run is separate from manage because computing a scenario costs server time and
-- is a distinct act from defining one, matching the workflow permissions.
INSERT INTO permissions (key, category, description) VALUES
('simulation.read',   'simulation', 'View saved simulation scenarios and their run history'),
('simulation.manage', 'simulation', 'Create, edit and delete simulation scenarios'),
('simulation.run',    'simulation', 'Compute a simulation scenario, including a preview')
ON CONFLICT (key) DO NOTHING;

-- simulation.read: roles whose purpose includes looking at the numbers.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'simulation.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'executive',
                'sales_manager', 'support_manager', 'analyst', 'data_steward',
                'readonly_auditor', 'security_auditor')
ON CONFLICT DO NOTHING;

-- simulation.manage: the roles that model the business.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'simulation.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager', 'analyst')
ON CONFLICT DO NOTHING;

-- simulation.run: the roles that may compute a scenario.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'simulation.run'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager', 'analyst')
ON CONFLICT DO NOTHING;
