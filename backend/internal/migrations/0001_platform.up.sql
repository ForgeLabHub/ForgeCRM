-- 0001_platform: identity, tenancy, RBAC, sessions, audit, jobs, outbox.
--
-- Design notes:
--   * Every tenant-scoped table carries organization_id so that isolation is a
--     predicate, not a convention.
--   * Roles with a NULL organization_id are platform-level roles; roles with an
--     organization_id are tenant roles seeded from the same permission catalog.
--   * Session tokens are never stored; only a SHA-256 hash is persisted.

CREATE EXTENSION IF NOT EXISTS citext;

-- Keeps updated_at honest without relying on every call site.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Tenancy
-- ---------------------------------------------------------------------------

CREATE TABLE organizations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    slug        citext NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'archived')),
    settings    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);
CREATE TRIGGER organizations_updated_at BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Users
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email            citext NOT NULL UNIQUE,
    password_hash    text,
    display_name     text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
    status           text NOT NULL DEFAULT 'active'
                     CHECK (status IN ('invited', 'active', 'suspended', 'blocked')),
    is_platform_owner boolean NOT NULL DEFAULT false,
    mfa_enabled      boolean NOT NULL DEFAULT false,
    failed_attempts  integer NOT NULL DEFAULT 0,
    locked_until     timestamptz,
    last_login_at    timestamptz,
    locale           text NOT NULL DEFAULT 'en',
    timezone         text NOT NULL DEFAULT 'UTC',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX users_status_idx ON users (status);

-- Only one platform owner can exist; enforced rather than assumed.
CREATE UNIQUE INDEX users_single_platform_owner ON users ((is_platform_owner))
    WHERE is_platform_owner;

CREATE TABLE memberships (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    is_org_owner    boolean NOT NULL DEFAULT false,
    status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, organization_id)
);
CREATE TRIGGER memberships_updated_at BEFORE UPDATE ON memberships
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX memberships_org_idx ON memberships (organization_id);

CREATE TABLE teams (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    description     text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    UNIQUE (organization_id, name)
);
CREATE TRIGGER teams_updated_at BEFORE UPDATE ON teams
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE team_members (
    team_id  uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user_idx ON team_members (user_id);

-- ---------------------------------------------------------------------------
-- RBAC
-- ---------------------------------------------------------------------------

CREATE TABLE permissions (
    key         text PRIMARY KEY,
    category    text NOT NULL,
    description text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    key             text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,62}$'),
    name            text NOT NULL,
    description     text NOT NULL DEFAULT '',
    is_system       boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER roles_updated_at BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- NULL organization_id means a platform role; the sentinel keeps the uniqueness
-- constraint meaningful for both scopes.
CREATE UNIQUE INDEX roles_scope_key ON roles (
    COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid), key
);

CREATE TABLE role_permissions (
    role_id        uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_key text NOT NULL REFERENCES permissions(key) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_key)
);

CREATE TABLE user_roles (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id         uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    granted_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    granted_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, role_id)
);
CREATE INDEX user_roles_user_idx ON user_roles (user_id);
CREATE INDEX user_roles_org_idx ON user_roles (organization_id);

-- ---------------------------------------------------------------------------
-- Sessions and authentication factors
-- ---------------------------------------------------------------------------

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE,
    csrf_secret  bytea NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at   timestamptz,
    ip           inet,
    user_agent   text,
    mfa_verified boolean NOT NULL DEFAULT false
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE mfa_factors (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type         text NOT NULL DEFAULT 'totp' CHECK (type IN ('totp')),
    secret       text NOT NULL,
    confirmed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, type)
);

CREATE TABLE recovery_codes (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    used_at   timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

CREATE TABLE password_reset_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX password_reset_user_idx ON password_reset_tokens (user_id);

CREATE TABLE login_attempts (
    id         bigserial PRIMARY KEY,
    email      citext NOT NULL,
    ip         inet,
    success    boolean NOT NULL,
    reason     text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_email_idx ON login_attempts (email, created_at DESC);

-- ---------------------------------------------------------------------------
-- Audit and security events (separate streams, as required)
-- ---------------------------------------------------------------------------

CREATE TABLE audit_log (
    id              bigserial PRIMARY KEY,
    organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
    actor_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    actor_type      text NOT NULL DEFAULT 'user' CHECK (actor_type IN ('user', 'system', 'api_key', 'ai_agent')),
    action          text NOT NULL,
    entity_type     text NOT NULL,
    entity_id       text,
    before          jsonb,
    after           jsonb,
    metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
    correlation_id  text,
    ip              inet,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_org_time_idx ON audit_log (organization_id, created_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_user_id, created_at DESC);
CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id);
CREATE INDEX audit_log_action_idx ON audit_log (action, created_at DESC);

-- Append-only: block UPDATE and DELETE at the database level.
CREATE OR REPLACE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();

CREATE TABLE security_events (
    id             bigserial PRIMARY KEY,
    organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
    user_id        uuid REFERENCES users(id) ON DELETE SET NULL,
    event_type     text NOT NULL,
    severity       text NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warn', 'error', 'critical')),
    message        text NOT NULL,
    metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip             inet,
    user_agent     text,
    correlation_id text,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX security_events_time_idx ON security_events (created_at DESC);
CREATE INDEX security_events_type_idx ON security_events (event_type, created_at DESC);
CREATE INDEX security_events_user_idx ON security_events (user_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Application log mirror (structured logs retained for the admin UI)
-- ---------------------------------------------------------------------------

CREATE TABLE app_logs (
    id             bigserial PRIMARY KEY,
    level          text NOT NULL CHECK (level IN ('debug', 'info', 'warn', 'error')),
    component      text NOT NULL DEFAULT '',
    message        text NOT NULL,
    fields         jsonb NOT NULL DEFAULT '{}'::jsonb,
    organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
    user_id        uuid REFERENCES users(id) ON DELETE SET NULL,
    correlation_id text,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX app_logs_time_idx ON app_logs (created_at DESC);
CREATE INDEX app_logs_level_idx ON app_logs (level, created_at DESC);
CREATE INDEX app_logs_component_idx ON app_logs (component, created_at DESC);

-- ---------------------------------------------------------------------------
-- Background jobs
-- ---------------------------------------------------------------------------

CREATE TABLE jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    kind            text NOT NULL,
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    priority        integer NOT NULL DEFAULT 100,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    checkpoint      jsonb NOT NULL DEFAULT '{}'::jsonb,
    progress        jsonb NOT NULL DEFAULT '{}'::jsonb,
    result          jsonb,
    attempts        integer NOT NULL DEFAULT 0,
    max_attempts    integer NOT NULL DEFAULT 5,
    run_after       timestamptz NOT NULL DEFAULT now(),
    locked_at       timestamptz,
    locked_by       text,
    started_at      timestamptz,
    finished_at     timestamptz,
    last_error      text,
    cancel_requested boolean NOT NULL DEFAULT false,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER jobs_updated_at BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX jobs_claim_idx ON jobs (status, run_after, priority) WHERE status = 'queued';
CREATE INDEX jobs_org_idx ON jobs (organization_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Transactional outbox
-- ---------------------------------------------------------------------------

CREATE TABLE outbox_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    event_type      text NOT NULL,
    schema_version  integer NOT NULL DEFAULT 1,
    source          text NOT NULL DEFAULT 'forgecrm',
    payload         jsonb NOT NULL,
    correlation_id  text,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    published_at    timestamptz,
    attempts        integer NOT NULL DEFAULT 0,
    last_error      text
);
CREATE INDEX outbox_pending_idx ON outbox_events (occurred_at) WHERE published_at IS NULL;

-- ---------------------------------------------------------------------------
-- Inbound idempotency
-- ---------------------------------------------------------------------------

CREATE TABLE idempotency_keys (
    scope       text NOT NULL,
    key         text NOT NULL,
    response    jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, key)
);
CREATE INDEX idempotency_created_idx ON idempotency_keys (created_at);

-- ---------------------------------------------------------------------------
-- System settings
-- ---------------------------------------------------------------------------

CREATE TABLE app_settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
