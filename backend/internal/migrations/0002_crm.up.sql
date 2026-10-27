-- 0002_crm: core CRM entities.
--
-- Design notes:
--   * External identifiers live in (external_source, external_id) with a unique
--     constraint scoped to the organization. They are never used as primary keys.
--   * Archiving is a timestamp, not a delete, so history is preserved.
--   * owner_user_id supports record-level access rules (owner / team scoping).

-- ---------------------------------------------------------------------------
-- Accounts (client organisations) and contacts
-- ---------------------------------------------------------------------------

CREATE TABLE accounts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 300),
    legal_name      text,
    industry        text,
    website         text,
    phone           text,
    email           citext,
    tax_id          text,
    address_line1   text,
    address_line2   text,
    city            text,
    region          text,
    postal_code     text,
    country         text,
    annual_revenue  numeric(18,2),
    employee_count  integer CHECK (employee_count IS NULL OR employee_count >= 0),
    owner_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    team_id         uuid REFERENCES teams(id) ON DELETE SET NULL,
    parent_account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
    external_source text,
    external_id     text,
    custom_fields   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE TRIGGER accounts_updated_at BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX accounts_external_uniq ON accounts (organization_id, external_source, external_id)
    WHERE external_source IS NOT NULL AND external_id IS NOT NULL;
CREATE INDEX accounts_org_name_idx ON accounts (organization_id, name);
CREATE INDEX accounts_org_created_idx ON accounts (organization_id, created_at DESC);
CREATE INDEX accounts_owner_idx ON accounts (owner_user_id);
CREATE INDEX accounts_active_idx ON accounts (organization_id) WHERE archived_at IS NULL;

CREATE TABLE contacts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id      uuid REFERENCES accounts(id) ON DELETE SET NULL,
    first_name      text,
    last_name       text,
    email           citext,
    phone           text,
    mobile          text,
    job_title       text,
    department      text,
    is_primary      boolean NOT NULL DEFAULT false,
    owner_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    team_id         uuid REFERENCES teams(id) ON DELETE SET NULL,
    external_source text,
    external_id     text,
    custom_fields   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    CONSTRAINT contacts_needs_name CHECK (
        (first_name IS NOT NULL AND btrim(first_name) <> '')
        OR (last_name IS NOT NULL AND btrim(last_name) <> '')
        OR (email IS NOT NULL)
    )
);
CREATE TRIGGER contacts_updated_at BEFORE UPDATE ON contacts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX contacts_external_uniq ON contacts (organization_id, external_source, external_id)
    WHERE external_source IS NOT NULL AND external_id IS NOT NULL;
CREATE INDEX contacts_org_created_idx ON contacts (organization_id, created_at DESC);
CREATE INDEX contacts_account_idx ON contacts (account_id);
CREATE INDEX contacts_email_idx ON contacts (organization_id, email);
CREATE INDEX contacts_active_idx ON contacts (organization_id) WHERE archived_at IS NULL;

-- ---------------------------------------------------------------------------
-- Leads
-- ---------------------------------------------------------------------------

CREATE TABLE leads (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    title           text NOT NULL DEFAULT '',
    first_name      text,
    last_name       text,
    email           citext,
    phone           text,
    company         text,
    source          text,
    status          text NOT NULL DEFAULT 'new'
                    CHECK (status IN ('new', 'contacted', 'qualified', 'unqualified', 'converted')),
    score           integer,
    notes           text,
    owner_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    team_id         uuid REFERENCES teams(id) ON DELETE SET NULL,
    converted_account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
    converted_contact_id uuid REFERENCES contacts(id) ON DELETE SET NULL,
    converted_at    timestamptz,
    external_source text,
    external_id     text,
    custom_fields   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE TRIGGER leads_updated_at BEFORE UPDATE ON leads
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX leads_external_uniq ON leads (organization_id, external_source, external_id)
    WHERE external_source IS NOT NULL AND external_id IS NOT NULL;
CREATE INDEX leads_org_status_idx ON leads (organization_id, status, created_at DESC);
CREATE INDEX leads_owner_idx ON leads (owner_user_id);

-- ---------------------------------------------------------------------------
-- Pipelines and deals
-- ---------------------------------------------------------------------------

CREATE TABLE pipelines (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    is_default      boolean NOT NULL DEFAULT false,
    currency        text NOT NULL DEFAULT 'USD' CHECK (length(currency) = 3),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    UNIQUE (organization_id, name)
);
CREATE TRIGGER pipelines_updated_at BEFORE UPDATE ON pipelines
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX pipelines_one_default ON pipelines (organization_id)
    WHERE is_default AND archived_at IS NULL;

CREATE TABLE pipeline_stages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id     uuid NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    position        integer NOT NULL DEFAULT 0,
    probability     integer NOT NULL DEFAULT 0 CHECK (probability BETWEEN 0 AND 100),
    is_won          boolean NOT NULL DEFAULT false,
    is_lost         boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_id, name),
    CONSTRAINT stage_terminal_exclusive CHECK (NOT (is_won AND is_lost))
);
CREATE TRIGGER pipeline_stages_updated_at BEFORE UPDATE ON pipeline_stages
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX pipeline_stages_order_idx ON pipeline_stages (pipeline_id, position);

CREATE TABLE deals (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pipeline_id     uuid NOT NULL REFERENCES pipelines(id) ON DELETE RESTRICT,
    stage_id        uuid NOT NULL REFERENCES pipeline_stages(id) ON DELETE RESTRICT,
    account_id      uuid REFERENCES accounts(id) ON DELETE SET NULL,
    primary_contact_id uuid REFERENCES contacts(id) ON DELETE SET NULL,
    title           text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    description     text,
    amount          numeric(18,2) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    currency        text NOT NULL DEFAULT 'USD' CHECK (length(currency) = 3),
    discount_percent numeric(5,2) NOT NULL DEFAULT 0 CHECK (discount_percent BETWEEN 0 AND 100),
    discount_approved_by uuid REFERENCES users(id) ON DELETE SET NULL,
    discount_approved_at timestamptz,
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'won', 'lost')),
    expected_close_date date,
    closed_at       timestamptz,
    lost_reason     text,
    owner_user_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    team_id         uuid REFERENCES teams(id) ON DELETE SET NULL,
    external_source text,
    external_id     text,
    custom_fields   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE TRIGGER deals_updated_at BEFORE UPDATE ON deals
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX deals_external_uniq ON deals (organization_id, external_source, external_id)
    WHERE external_source IS NOT NULL AND external_id IS NOT NULL;
CREATE INDEX deals_org_status_idx ON deals (organization_id, status, created_at DESC);
CREATE INDEX deals_stage_idx ON deals (stage_id);
CREATE INDEX deals_account_idx ON deals (account_id);
CREATE INDEX deals_owner_idx ON deals (owner_user_id);
CREATE INDEX deals_expected_close_idx ON deals (organization_id, expected_close_date)
    WHERE status = 'open';

CREATE TABLE deal_stage_history (
    id          bigserial PRIMARY KEY,
    deal_id     uuid NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    from_stage_id uuid REFERENCES pipeline_stages(id) ON DELETE SET NULL,
    to_stage_id   uuid NOT NULL REFERENCES pipeline_stages(id) ON DELETE RESTRICT,
    changed_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    note        text,
    changed_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deal_stage_history_deal_idx ON deal_stage_history (deal_id, changed_at DESC);

-- ---------------------------------------------------------------------------
-- Tasks, activities, tickets
-- ---------------------------------------------------------------------------

CREATE TABLE tasks (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    title           text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    description     text,
    status          text NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open', 'in_progress', 'completed', 'cancelled')),
    priority        text NOT NULL DEFAULT 'normal'
                    CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    due_at          timestamptz,
    completed_at    timestamptz,
    assignee_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    team_id         uuid REFERENCES teams(id) ON DELETE SET NULL,
    account_id      uuid REFERENCES accounts(id) ON DELETE CASCADE,
    contact_id      uuid REFERENCES contacts(id) ON DELETE CASCADE,
    deal_id         uuid REFERENCES deals(id) ON DELETE CASCADE,
    ticket_id       uuid,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE TRIGGER tasks_updated_at BEFORE UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX tasks_org_status_idx ON tasks (organization_id, status, due_at);
CREATE INDEX tasks_assignee_idx ON tasks (assignee_user_id, status);
CREATE INDEX tasks_deal_idx ON tasks (deal_id);
CREATE INDEX tasks_account_idx ON tasks (account_id);

CREATE TABLE activities (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    type            text NOT NULL CHECK (type IN ('call', 'meeting', 'note', 'email', 'task', 'stage_change', 'system')),
    subject         text NOT NULL DEFAULT '',
    body            text,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    duration_minutes integer CHECK (duration_minutes IS NULL OR duration_minutes >= 0),
    outcome         text,
    account_id      uuid REFERENCES accounts(id) ON DELETE CASCADE,
    contact_id      uuid REFERENCES contacts(id) ON DELETE CASCADE,
    deal_id         uuid REFERENCES deals(id) ON DELETE CASCADE,
    ticket_id       uuid,
    user_id         uuid REFERENCES users(id) ON DELETE SET NULL,
    external_source text,
    external_id     text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER activities_updated_at BEFORE UPDATE ON activities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX activities_external_uniq ON activities (organization_id, external_source, external_id)
    WHERE external_source IS NOT NULL AND external_id IS NOT NULL;
CREATE INDEX activities_org_time_idx ON activities (organization_id, occurred_at DESC);
CREATE INDEX activities_account_idx ON activities (account_id, occurred_at DESC);
CREATE INDEX activities_deal_idx ON activities (deal_id, occurred_at DESC);

CREATE TABLE sla_policies (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                text NOT NULL,
    priority            text NOT NULL CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    first_response_minutes integer NOT NULL CHECK (first_response_minutes > 0),
    resolution_minutes     integer NOT NULL CHECK (resolution_minutes > 0),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);
CREATE TRIGGER sla_policies_updated_at BEFORE UPDATE ON sla_policies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE tickets (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    number          bigserial,
    subject         text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 300),
    description     text,
    status          text NOT NULL DEFAULT 'new'
                    CHECK (status IN ('new', 'open', 'pending', 'resolved', 'closed')),
    priority        text NOT NULL DEFAULT 'normal'
                    CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    channel         text NOT NULL DEFAULT 'web' CHECK (channel IN ('web', 'email', 'phone', 'telegram', 'api')),
    account_id      uuid REFERENCES accounts(id) ON DELETE SET NULL,
    contact_id      uuid REFERENCES contacts(id) ON DELETE SET NULL,
    assignee_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    team_id         uuid REFERENCES teams(id) ON DELETE SET NULL,
    sla_policy_id   uuid REFERENCES sla_policies(id) ON DELETE SET NULL,
    first_response_due_at timestamptz,
    resolution_due_at     timestamptz,
    first_responded_at    timestamptz,
    resolved_at     timestamptz,
    sla_breached    boolean NOT NULL DEFAULT false,
    external_source text,
    external_id     text,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    UNIQUE (organization_id, number)
);
CREATE TRIGGER tickets_updated_at BEFORE UPDATE ON tickets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX tickets_external_uniq ON tickets (organization_id, external_source, external_id)
    WHERE external_source IS NOT NULL AND external_id IS NOT NULL;
CREATE INDEX tickets_org_status_idx ON tickets (organization_id, status, created_at DESC);
CREATE INDEX tickets_assignee_idx ON tickets (assignee_user_id, status);

-- ---------------------------------------------------------------------------
-- Contracts and documents
-- ---------------------------------------------------------------------------

CREATE TABLE contracts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id      uuid REFERENCES accounts(id) ON DELETE SET NULL,
    deal_id         uuid REFERENCES deals(id) ON DELETE SET NULL,
    title           text NOT NULL,
    status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'pending_approval', 'active', 'expired', 'terminated')),
    value           numeric(18,2) CHECK (value IS NULL OR value >= 0),
    currency        text NOT NULL DEFAULT 'USD',
    starts_on       date,
    ends_on         date,
    document_id     uuid,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    CONSTRAINT contract_dates CHECK (ends_on IS NULL OR starts_on IS NULL OR ends_on >= starts_on)
);
CREATE TRIGGER contracts_updated_at BEFORE UPDATE ON contracts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX contracts_org_status_idx ON contracts (organization_id, status);

CREATE TABLE documents (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    filename        text NOT NULL,
    content_type    text NOT NULL DEFAULT 'application/octet-stream',
    size_bytes      bigint NOT NULL CHECK (size_bytes >= 0),
    checksum_sha256 text NOT NULL CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    storage_path    text NOT NULL,
    title           text,
    account_id      uuid REFERENCES accounts(id) ON DELETE SET NULL,
    deal_id         uuid REFERENCES deals(id) ON DELETE SET NULL,
    contact_id      uuid REFERENCES contacts(id) ON DELETE SET NULL,
    uploaded_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE INDEX documents_org_idx ON documents (organization_id, created_at DESC);
CREATE INDEX documents_account_idx ON documents (account_id);

ALTER TABLE contracts
    ADD CONSTRAINT contracts_document_fk FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Tags, comments, custom fields
-- ---------------------------------------------------------------------------

CREATE TABLE tags (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    color           text NOT NULL DEFAULT '#64748b',
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE TABLE taggables (
    tag_id      uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    entity_type text NOT NULL,
    entity_id   uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tag_id, entity_type, entity_id)
);
CREATE INDEX taggables_entity_idx ON taggables (entity_type, entity_id);

CREATE TABLE comments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    entity_type     text NOT NULL,
    entity_id       uuid NOT NULL,
    body            text NOT NULL CHECK (length(btrim(body)) > 0),
    author_user_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE TRIGGER comments_updated_at BEFORE UPDATE ON comments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX comments_entity_idx ON comments (entity_type, entity_id, created_at DESC);

CREATE TABLE custom_field_definitions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    entity_type     text NOT NULL CHECK (entity_type IN ('account', 'contact', 'lead', 'deal', 'ticket', 'task')),
    key             text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,62}$'),
    label           text NOT NULL,
    field_type      text NOT NULL CHECK (field_type IN ('text', 'number', 'boolean', 'date', 'select', 'multiselect')),
    options         jsonb NOT NULL DEFAULT '[]'::jsonb,
    required        boolean NOT NULL DEFAULT false,
    position        integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,
    UNIQUE (organization_id, entity_type, key)
);
CREATE TRIGGER custom_field_definitions_updated_at BEFORE UPDATE ON custom_field_definitions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Notifications and approvals
-- ---------------------------------------------------------------------------

CREATE TABLE notifications (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type            text NOT NULL,
    title           text NOT NULL,
    body            text,
    entity_type     text,
    entity_id       uuid,
    read_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, read_at, created_at DESC);

CREATE TABLE approvals (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    entity_type     text NOT NULL,
    entity_id       uuid NOT NULL,
    kind            text NOT NULL,
    status          text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    requested_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    decided_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    decision_note   text,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz
);
CREATE INDEX approvals_status_idx ON approvals (organization_id, status, created_at DESC);
CREATE INDEX approvals_entity_idx ON approvals (entity_type, entity_id);
