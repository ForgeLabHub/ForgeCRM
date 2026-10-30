-- 0027_consent: a record of what a person agreed to, and the policy that reads it.
--
-- Until now nothing recorded consent. Every outbound channel — email, Telegram, a
-- webhook — could contact a person without any stored evidence that they agreed to
-- be contacted, and nothing could answer "why was this allowed?" afterwards. This
-- migration adds the record and the policy that reads it.
--
-- The model is deliberately small. A consent record is one subject (a contact, or a
-- bare email address for someone not yet in the CRM), one purpose (marketing,
-- transactional, …) and one channel (email, telegram, webhook). Its state is a
-- three-way value rather than a boolean:
--
--     granted    the person agreed
--     denied     the person refused
--     withdrawn  the person agreed once and later withdrew
--
-- A boolean would have to encode "never asked" as false, which is exactly the
-- confusion this feature exists to remove: silence is not refusal, and it is not
-- permission either. The absent row is "never asked", and the policy says so.
--
-- The row is append-only in spirit: a change of mind writes a new revision and
-- marks the previous one superseded, so the history of what a person was told and
-- when they agreed is never rewritten. `revision` and `superseded_by` carry that.
--
-- An email subject that is not a contact is keyed by `subject_email` (citext, so it
-- matches the case-insensitive column on contacts). A subject that is a contact is
-- keyed by `contact_id`. Exactly one of the two is set, enforced by a CHECK.

CREATE TABLE consent_records (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- The person. Either a contact already in the CRM, or an email address for a
    -- person who is not (an unsubscribe link arrives from a mailbox, not an id).
    contact_id      uuid REFERENCES contacts(id) ON DELETE SET NULL,
    subject_email   citext,
    -- What they agreed to. The set is closed in the application; a free-text purpose
    -- would make "may we email them about X" unanswerable by a query.
    purpose         text NOT NULL CHECK (purpose IN
                        ('marketing', 'transactional', 'product_updates', 'research')),
    -- How we would reach them. The channel is part of the key because agreeing to be
    -- emailed is not agreeing to be messaged on Telegram.
    channel         text NOT NULL CHECK (channel IN ('email', 'telegram', 'webhook')),
    state           text NOT NULL CHECK (state IN ('granted', 'denied', 'withdrawn')),
    -- The lawful basis or the source, in the operator's own words. Free text because
    -- jurisdictions name these differently and the platform should not pretend to
    -- enumerate them. It is what an operator points at when asked "on what grounds?".
    basis           text,
    -- Where the decision came from, e.g. "signup form", "import", "support call".
    source          text,
    -- The consent text the person actually saw, when it is known. Storing it is what
    -- makes "they agreed to this wording" checkable rather than assumed.
    evidence        text,
    -- The revision of this (subject, purpose, channel) decision. A change of mind is a
    -- new row with revision + 1, and the old row points at it through superseded_by,
    -- so the chain of what happened is preserved rather than overwritten.
    revision        integer NOT NULL DEFAULT 1 CHECK (revision >= 1),
    superseded_by   uuid REFERENCES consent_records(id) ON DELETE SET NULL,
    -- Who recorded it. NULL for a system action (an import, a workflow).
    recorded_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    -- When the decision is effective. A decision may be recorded now and take effect
    -- later, or record a past agreement discovered during migration.
    decided_at      timestamptz NOT NULL DEFAULT now(),
    -- When it stops mattering, if it does. NULL means it does not expire.
    expires_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    -- Exactly one subject key. A row with both would be ambiguous about which one the
    -- lookup matched, and a row with neither is about nobody.
    CONSTRAINT consent_records_subject CHECK (
        (contact_id IS NOT NULL AND subject_email IS NULL)
        OR (contact_id IS NULL AND subject_email IS NOT NULL)
    )
);

CREATE TRIGGER consent_records_updated_at BEFORE UPDATE ON consent_records
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The read the policy performs: the current decision for one subject, purpose and
-- channel. The partial index keeps only rows that are still in force, which is what
-- a policy check asks for; a superseded row is history and is not consulted on the
-- hot path.
CREATE INDEX consent_records_current_idx
    ON consent_records (organization_id, contact_id, subject_email, purpose, channel)
    WHERE superseded_by IS NULL;

-- The listing an operator works through, newest first.
CREATE INDEX consent_records_listing_idx
    ON consent_records (organization_id, created_at DESC);

-- A retention sweep deletes by created_at across organizations, so it needs an index
-- that leads with that column; the listing index leads with organization_id.
CREATE INDEX consent_records_retention_idx ON consent_records (created_at);

-- Permission catalog. Reading consent (who agreed to what) is an audit and
-- compliance activity; recording a decision changes whether a person can be
-- contacted, so the two are separate rights. Neither is a CRM write: a consent
-- record is evidence about a person, not a change to their CRM record.
INSERT INTO permissions (key, category, description) VALUES
('consent.read',   'consent', 'View consent records and the reason a send was allowed or blocked'),
('consent.manage', 'consent', 'Record, change and withdraw consent decisions')
ON CONFLICT (key) DO NOTHING;

-- consent.read: everyone who works with people or runs automation needs to be able
-- to answer "may we contact them?", and an auditor needs the whole history.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'consent.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'sales_manager',
                'sales_representative', 'support_agent', 'support_manager', 'data_steward',
                'executive', 'security_auditor', 'readonly_auditor')
ON CONFLICT DO NOTHING;

-- consent.manage: recording and withdrawing a decision is a deliberate act with
-- legal consequence, so it is narrower than reading it.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'consent.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'sales_manager',
                'data_steward')
ON CONFLICT DO NOTHING;
