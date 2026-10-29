-- 0020_forecasting_risk: stored forecasts, persisted risk signals with their
-- status, the per-organization risk thresholds, and the permission catalog
-- entries that guard them.
--
-- Three tables plus a settings row.
--
-- A forecast_run is one computation of a forecast against a frozen history. Like
-- a simulation run it is immutable: it keeps the exact series, the method, the
-- horizon and the algorithm version, so a forecast stays interpretable after the
-- CRM data moved on and two runs that differ can be told apart by their inputs
-- rather than guessed at. Money is stored as integer minor units inside the
-- result jsonb, never a float.
--
-- A risk_signal is one explainable risk, with a status an operator moves through
-- acknowledge/dismiss/resolve. A scan upserts the signals that fired and resolves
-- the ones that no longer do, so a signal is not duplicated on every scan and an
-- operator's acknowledgement survives a re-scan. The unique index that makes the
-- upsert safe is partial, over the non-terminal statuses only: a resolved signal
-- is history, and a fresh occurrence of the same condition is a new row.
--
-- Nothing here can modify CRM data. These tables hold the platform's own analysis
-- inputs and outputs; the CRM is read, never written, by this path.

CREATE TABLE forecast_runs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    method            text NOT NULL CHECK (method IN ('mean', 'moving_average', 'trend')),
    algorithm_version text NOT NULL,
    horizon           integer NOT NULL CHECK (horizon BETWEEN 1 AND 36),
    -- params is the full parameter set as supplied (including the moving-average
    -- window). history is the observed series the forecast was computed from.
    params            jsonb NOT NULL DEFAULT '{}'::jsonb,
    history           jsonb NOT NULL DEFAULT '[]'::jsonb,
    result            jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- A scenario-like list: the organization's recent forecasts, newest first.
CREATE INDEX forecast_runs_org_idx
    ON forecast_runs (organization_id, created_at DESC);

CREATE TABLE risk_signals (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    detected_at       timestamptz NOT NULL DEFAULT now(),
    type              text NOT NULL CHECK (type IN (
                          'stalled_deal', 'overdue_task', 'account_without_contact',
                          'amount_spike', 'conversion_drop', 'stage_concentration')),
    severity          text NOT NULL CHECK (severity IN ('info', 'warn', 'high', 'critical')),
    rule              text NOT NULL,
    algorithm_version text NOT NULL,
    entity_type       text NOT NULL,
    entity_id         text NOT NULL,
    entity_label      text NOT NULL DEFAULT '',
    rationale         text NOT NULL,
    -- evidence is the observed values the decision used, so the signal can be
    -- explained without recomputing it.
    evidence          jsonb NOT NULL DEFAULT '[]'::jsonb,
    status            text NOT NULL DEFAULT 'open'
                      CHECK (status IN ('open', 'acknowledged', 'resolved', 'dismissed')),
    status_changed_at timestamptz,
    status_changed_by uuid REFERENCES users(id) ON DELETE SET NULL,
    -- resolved_by_scan marks a signal the platform closed because its condition no
    -- longer holds, as distinct from one an operator resolved. Kept so the two are
    -- not confused when reviewing the history.
    resolved_by_scan  boolean NOT NULL DEFAULT false
);

-- The upsert target: at most one live signal per rule and entity. Partial, over the
-- non-terminal statuses, so a resolved signal does not block a new occurrence.
CREATE UNIQUE INDEX risk_signals_live_uniq
    ON risk_signals (organization_id, rule, entity_type, entity_id)
    WHERE status IN ('open', 'acknowledged');

-- The signal list filters by status and severity and orders by detection time.
CREATE INDEX risk_signals_org_status_idx
    ON risk_signals (organization_id, status, severity, detected_at DESC);

-- The scan reconciles by rule, so it looks up live signals by rule.
CREATE INDEX risk_signals_org_rule_idx
    ON risk_signals (organization_id, rule)
    WHERE status IN ('open', 'acknowledged');

-- Retention index: the sweep removes terminal signals by when their status last
-- changed, so it needs an index on that column restricted to terminal rows.
CREATE INDEX risk_signals_terminal_idx
    ON risk_signals (status_changed_at)
    WHERE status IN ('resolved', 'dismissed');

CREATE TABLE risk_thresholds (
    organization_id          uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    stalled_deal_days        integer NOT NULL DEFAULT 30 CHECK (stalled_deal_days BETWEEN 1 AND 3650),
    overdue_task_days        integer NOT NULL DEFAULT 7  CHECK (overdue_task_days BETWEEN 1 AND 3650),
    account_no_contact_days  integer NOT NULL DEFAULT 60 CHECK (account_no_contact_days BETWEEN 1 AND 3650),
    -- Ratios are basis points (10000 = 1.0), matching internal/analytics.Ratio, so
    -- the stored value and the computed value are the same number.
    amount_spike_ratio        integer NOT NULL DEFAULT 30000 CHECK (amount_spike_ratio BETWEEN 10000 AND 1000000),
    conversion_drop_ratio     integer NOT NULL DEFAULT 5000  CHECK (conversion_drop_ratio BETWEEN 0 AND 10000),
    stage_concentration_ratio integer NOT NULL DEFAULT 7000  CHECK (stage_concentration_ratio BETWEEN 0 AND 10000),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    updated_by               uuid REFERENCES users(id) ON DELETE SET NULL
);

CREATE TRIGGER risk_thresholds_updated_at BEFORE UPDATE ON risk_thresholds
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Permission catalog entries. Same shape as migrations 0006, 0013 and 0019: the
-- names are added to the Go catalog (internal/rbac) in the same change as this
-- migration, and TestSeedDataMatchesThePermissionCatalog fails if the two disagree.
--
--   forecast.read   view stored forecasts and the forecast page.
--   forecast.run    compute a forecast (spends compute against the whole history).
--   risk.read       view risk signals and scan for them.
--   risk.manage     change the organization's risk thresholds.
INSERT INTO permissions (key, category, description) VALUES
('forecast.read', 'forecast', 'View stored forecasts'),
('forecast.run',  'forecast', 'Compute a forecast'),
('risk.read',     'risk',     'View risk signals and run a risk scan'),
('risk.manage',   'risk',     'Change the organization risk thresholds')
ON CONFLICT (key) DO NOTHING;

-- forecast.read / risk.read: roles whose purpose includes looking at the numbers.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, p.key
FROM roles r
CROSS JOIN (VALUES ('forecast.read'), ('risk.read')) AS p(key)
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'executive',
                'sales_manager', 'support_manager', 'analyst', 'data_steward',
                'readonly_auditor', 'security_auditor')
ON CONFLICT DO NOTHING;

-- forecast.run: the roles that may spend compute on a forecast.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'forecast.run'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager', 'analyst')
ON CONFLICT DO NOTHING;

-- risk.manage: the roles that set the organization's thresholds.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'risk.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'sales_manager', 'support_manager', 'data_steward')
ON CONFLICT DO NOTHING;
