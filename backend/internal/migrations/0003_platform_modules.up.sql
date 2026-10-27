-- 0003_platform_modules: imports, migrations, integrations, events, notifications, AI.
--
-- Secret handling: columns holding credentials are named *_encrypted and store
-- AES-GCM ciphertext produced with the platform secret key. A plaintext password
-- must never be written to these tables or to any log.

-- ---------------------------------------------------------------------------
-- Data Import Center
-- ---------------------------------------------------------------------------

CREATE TABLE import_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL DEFAULT '',
    source_type     text NOT NULL CHECK (source_type IN ('file', 'database', 'rest_api')),
    entity_type     text NOT NULL CHECK (entity_type IN ('account', 'contact', 'lead', 'deal', 'task', 'ticket', 'activity')),
    format          text NOT NULL DEFAULT 'unknown',
    status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'analyzed', 'validated', 'dry_run', 'queued',
                                      'running', 'succeeded', 'failed', 'cancelled')),
    original_filename text,
    stored_path     text,
    checksum_sha256 text CHECK (checksum_sha256 IS NULL OR checksum_sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes      bigint CHECK (size_bytes IS NULL OR size_bytes >= 0),
    config          jsonb NOT NULL DEFAULT '{}'::jsonb,
    mapping         jsonb NOT NULL DEFAULT '{}'::jsonb,
    options         jsonb NOT NULL DEFAULT '{}'::jsonb,
    schema_info     jsonb NOT NULL DEFAULT '{}'::jsonb,
    preview         jsonb NOT NULL DEFAULT '[]'::jsonb,
    report          jsonb NOT NULL DEFAULT '{}'::jsonb,
    total_rows      bigint NOT NULL DEFAULT 0,
    imported_rows   bigint NOT NULL DEFAULT 0,
    skipped_rows    bigint NOT NULL DEFAULT 0,
    failed_rows     bigint NOT NULL DEFAULT 0,
    job_id          uuid REFERENCES jobs(id) ON DELETE SET NULL,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    error           text
);
CREATE TRIGGER import_jobs_updated_at BEFORE UPDATE ON import_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX import_jobs_org_idx ON import_jobs (organization_id, created_at DESC);
CREATE INDEX import_jobs_status_idx ON import_jobs (status);

CREATE TABLE import_row_errors (
    id             bigserial PRIMARY KEY,
    import_job_id  uuid NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    row_number     bigint NOT NULL,
    error_code     text NOT NULL,
    message        text NOT NULL,
    raw_row        jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX import_row_errors_job_idx ON import_row_errors (import_job_id, row_number);

CREATE TABLE import_templates (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL,
    entity_type     text NOT NULL,
    mapping         jsonb NOT NULL DEFAULT '{}'::jsonb,
    options         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);
CREATE TRIGGER import_templates_updated_at BEFORE UPDATE ON import_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE export_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    entity_type     text NOT NULL,
    format          text NOT NULL DEFAULT 'csv' CHECK (format IN ('csv', 'json', 'jsonl')),
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    filters         jsonb NOT NULL DEFAULT '{}'::jsonb,
    row_count       bigint NOT NULL DEFAULT 0,
    stored_path     text,
    checksum_sha256 text,
    job_id          uuid REFERENCES jobs(id) ON DELETE SET NULL,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    error           text
);
CREATE INDEX export_jobs_org_idx ON export_jobs (organization_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Data migration from external systems
-- ---------------------------------------------------------------------------

CREATE TABLE migration_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    integration_id  uuid,
    name            text NOT NULL,
    source_kind     text NOT NULL CHECK (source_kind IN ('postgres', 'mysql', 'mssql', 'sqlite', 'oracle', 'rest_api')),
    strategy        text NOT NULL DEFAULT 'one_time'
                    CHECK (strategy IN ('one_time', 'periodic', 'incremental', 'cdc')),
    status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'analyzed', 'queued', 'running', 'succeeded',
                                      'failed', 'cancelled', 'paused')),
    config          jsonb NOT NULL DEFAULT '{}'::jsonb,
    mapping         jsonb NOT NULL DEFAULT '{}'::jsonb,
    watermark       jsonb NOT NULL DEFAULT '{}'::jsonb,
    report          jsonb NOT NULL DEFAULT '{}'::jsonb,
    job_id          uuid REFERENCES jobs(id) ON DELETE SET NULL,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    last_run_at     timestamptz,
    error           text
);
CREATE TRIGGER migration_jobs_updated_at BEFORE UPDATE ON migration_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX migration_jobs_org_idx ON migration_jobs (organization_id, created_at DESC);

CREATE TABLE migration_reconciliations (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_job_id  uuid NOT NULL REFERENCES migration_jobs(id) ON DELETE CASCADE,
    source_table      text NOT NULL,
    target_entity     text NOT NULL,
    source_count      bigint NOT NULL DEFAULT 0,
    target_count      bigint NOT NULL DEFAULT 0,
    matched_count     bigint NOT NULL DEFAULT 0,
    mismatched_count  bigint NOT NULL DEFAULT 0,
    source_checksum   text,
    target_checksum   text,
    status            text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'match', 'mismatch', 'error')),
    details           jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX migration_recon_job_idx ON migration_reconciliations (migration_job_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Integration hub
-- ---------------------------------------------------------------------------

CREATE TABLE integrations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    kind                text NOT NULL,
    direction           text NOT NULL DEFAULT 'inbound' CHECK (direction IN ('inbound', 'outbound', 'bidirectional')),
    status              text NOT NULL DEFAULT 'disabled'
                        CHECK (status IN ('enabled', 'disabled', 'error', 'revoked')),
    config              jsonb NOT NULL DEFAULT '{}'::jsonb,
    secrets_encrypted   bytea,
    allow_write         boolean NOT NULL DEFAULT false,
    timeout_seconds     integer NOT NULL DEFAULT 30 CHECK (timeout_seconds BETWEEN 1 AND 600),
    max_concurrency     integer NOT NULL DEFAULT 1 CHECK (max_concurrency BETWEEN 1 AND 64),
    quota_per_hour      integer,
    last_test_at        timestamptz,
    last_test_ok        boolean,
    last_test_error     text,
    last_sync_at        timestamptz,
    created_by          uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    archived_at         timestamptz,
    UNIQUE (organization_id, name)
);
CREATE TRIGGER integrations_updated_at BEFORE UPDATE ON integrations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX integrations_org_idx ON integrations (organization_id, status);

CREATE TABLE integration_runs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id uuid NOT NULL REFERENCES integrations(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    operation      text NOT NULL,
    status         text NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    records_read   bigint NOT NULL DEFAULT 0,
    records_written bigint NOT NULL DEFAULT 0,
    error          text,
    details        jsonb NOT NULL DEFAULT '{}'::jsonb,
    started_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz
);
CREATE INDEX integration_runs_integration_idx ON integration_runs (integration_id, started_at DESC);

-- ---------------------------------------------------------------------------
-- Events and webhooks
-- ---------------------------------------------------------------------------

CREATE TABLE events_inbox (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    event_id        text NOT NULL,
    event_type      text NOT NULL,
    schema_version  integer NOT NULL DEFAULT 1,
    source          text NOT NULL,
    payload         jsonb NOT NULL,
    correlation_id  text,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    received_at     timestamptz NOT NULL DEFAULT now(),
    processed_at    timestamptz,
    status          text NOT NULL DEFAULT 'received'
                    CHECK (status IN ('received', 'processed', 'failed', 'ignored')),
    error           text,
    UNIQUE (source, event_id)
);
CREATE INDEX events_inbox_status_idx ON events_inbox (status, received_at);
CREATE INDEX events_inbox_org_idx ON events_inbox (organization_id, received_at DESC);

CREATE TABLE webhook_subscriptions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL,
    target_url      text NOT NULL,
    event_types     text[] NOT NULL DEFAULT '{}',
    secret_encrypted bytea NOT NULL,
    enabled         boolean NOT NULL DEFAULT true,
    max_attempts    integer NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    disabled_at     timestamptz
);
CREATE TRIGGER webhook_subscriptions_updated_at BEFORE UPDATE ON webhook_subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX webhook_subs_org_idx ON webhook_subscriptions (organization_id, enabled);

CREATE TABLE webhook_deliveries (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    outbox_event_id uuid REFERENCES outbox_events(id) ON DELETE SET NULL,
    event_type      text NOT NULL,
    payload         jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'delivering', 'delivered', 'failed', 'dead')),
    attempts        integer NOT NULL DEFAULT 0,
    response_status integer,
    response_body   text,
    last_error      text,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    delivered_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_deliveries_pending_idx ON webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'failed');
CREATE INDEX webhook_deliveries_sub_idx ON webhook_deliveries (subscription_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Notifications delivery log
-- ---------------------------------------------------------------------------

CREATE TABLE email_log (
    id              bigserial PRIMARY KEY,
    organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
    to_address      citext NOT NULL,
    subject         text NOT NULL,
    template        text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'sent', 'failed')),
    attempts        integer NOT NULL DEFAULT 0,
    error           text,
    message_id      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz
);
CREATE INDEX email_log_status_idx ON email_log (status, created_at DESC);

CREATE TABLE telegram_links (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    telegram_user_id bigint NOT NULL,
    telegram_username text,
    link_code_hash  bytea,
    link_code_expires_at timestamptz,
    linked_at       timestamptz,
    revoked_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (telegram_user_id)
);
CREATE INDEX telegram_links_user_idx ON telegram_links (user_id);
CREATE INDEX telegram_links_code_idx ON telegram_links (link_code_hash);

-- ---------------------------------------------------------------------------
-- AI tasks
-- ---------------------------------------------------------------------------

CREATE TABLE ai_providers (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL,
    provider        text NOT NULL CHECK (provider IN ('ollama', 'openai_compatible')),
    base_url        text NOT NULL,
    model           text NOT NULL,
    api_key_encrypted bytea,
    enabled         boolean NOT NULL DEFAULT true,
    max_requests_per_minute integer NOT NULL DEFAULT 30 CHECK (max_requests_per_minute > 0),
    timeout_seconds integer NOT NULL DEFAULT 60 CHECK (timeout_seconds BETWEEN 1 AND 600),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);
CREATE TRIGGER ai_providers_updated_at BEFORE UPDATE ON ai_providers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ai_tasks (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         uuid REFERENCES users(id) ON DELETE SET NULL,
    provider        text NOT NULL,
    model           text NOT NULL,
    kind            text NOT NULL DEFAULT 'chat',
    prompt_summary  text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    input_tokens    integer,
    output_tokens   integer,
    cost_usd        numeric(12,6),
    latency_ms      integer,
    tool_calls      jsonb NOT NULL DEFAULT '[]'::jsonb,
    error           text,
    correlation_id  text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz
);
CREATE INDEX ai_tasks_org_idx ON ai_tasks (organization_id, created_at DESC);
CREATE INDEX ai_tasks_user_idx ON ai_tasks (user_id, created_at DESC);
