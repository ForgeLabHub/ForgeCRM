-- 0023_saved_views: named, reusable list filters ("views").
--
-- A saved view is one user's stored filter/sort/column selection for one CRM list
-- screen. It exists so that "my open deals this quarter" is a thing a person names
-- once and reopens, instead of re-entering four controls every visit.
--
-- A view may be private (owner_user_id set, shared = false) or shared with the
-- whole organization (shared = true, owner kept for "who made it"). Visibility is
-- therefore one predicate — owner = me OR shared — and the API enforces exactly
-- that, so a private view of another user is not readable even by an admin reading
-- the list, because it is not that admin's view.
--
-- The filter payload is jsonb and its shape is owned by internal/views. Storing it
-- as a document keeps a new filter field from needing a schema migration, the same
-- reasoning the workflow definition and the simulation parameters use. The payload
-- is validated on write against the entity type, so a view cannot carry a filter
-- the list endpoint would silently ignore.
--
-- (organization_id, owner_user_id, entity_type) is indexed because every read is
-- "the views I may use for this screen", and the entity type is always known.

CREATE TABLE saved_views (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type     text NOT NULL CHECK (entity_type IN
                        ('accounts', 'contacts', 'leads', 'deals', 'tasks', 'tickets')),
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    -- shared makes a view visible to everyone in the organization. It does not make
    -- it editable by everyone: only the owner may rename, update or delete it.
    shared          boolean NOT NULL DEFAULT false,
    -- The stored filter document: the list query parameters, the sort and the chosen
    -- columns. Shape owned by internal/views; validated on write.
    filters         jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- pinned views are surfaced at the top of the screen's view switcher.
    pinned          boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);

CREATE TRIGGER saved_views_updated_at BEFORE UPDATE ON saved_views
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Two users may both name a view "Hot leads" without colliding, so uniqueness is
-- per owner. A shared view is still owned by one person, so this stays correct.
CREATE UNIQUE INDEX saved_views_owner_name ON saved_views (owner_user_id, entity_type, lower(name))
    WHERE archived_at IS NULL;

-- The read pattern: every view this caller may use on this screen.
CREATE INDEX saved_views_scope_idx ON saved_views (organization_id, entity_type)
    WHERE archived_at IS NULL;
CREATE INDEX saved_views_owner_idx ON saved_views (owner_user_id, entity_type)
    WHERE archived_at IS NULL;

-- Permission catalog. A saved view is a personal convenience, so a single
-- permission covers the whole lifecycle: there is no "read someone else's private
-- view" that a separate right could grant.
INSERT INTO permissions (key, category, description) VALUES
('views.read',   'views', 'View saved list views'),
('views.manage', 'views', 'Create, edit and delete saved list views')
ON CONFLICT (key) DO NOTHING;

-- views.read: every role that works a CRM list may keep and open views.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'views.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'sales_manager',
                'sales_representative', 'data_steward', 'executive', 'security_auditor')
ON CONFLICT DO NOTHING;

-- views.manage: the roles that may author and edit their own views.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'views.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'sales_manager',
                'sales_representative', 'data_steward')
ON CONFLICT DO NOTHING;
