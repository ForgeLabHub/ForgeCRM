-- 0025_mcp_credentials: scoped, revocable credentials for external MCP clients.
--
-- Until now the MCP protocol endpoint authenticated with the browser session
-- cookie, which made a real MCP client impossible to run: a long-running client
-- cannot hold an interactive session, and an operator could not revoke an agent's
-- access without ending the person's own login. This table is the credential that
-- fixes both.
--
-- The credential is a personal access token scoped to one organization and owned
-- by one user. It is deliberately *narrower* than the owner: it carries an explicit
-- permission list, and a tool call is authorized against that list rather than the
-- user's roles. A credential created for read-only work cannot mutate CRM data even
-- if its owner is an administrator. That is the whole point of a separate
-- credential, and it is why an MCP client never receives the owner's authority by
-- default.
--
-- Only the SHA-256 of the token is stored. The plaintext is shown exactly once, at
-- creation, and is not recoverable afterwards. The first characters are kept as a
-- non-secret display prefix so an operator can tell two tokens apart in a list.
--
-- A credential may expire. NULL means it does not; a set value is enforced on every
-- call, and the retention sweep in internal/retention removes rows that can no
-- longer authenticate (revoked, or expired long ago).

CREATE TABLE mcp_credentials (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    -- The non-secret display prefix, e.g. "fcmcp_ab12cd34". Shown in the list so a
    -- token can be identified without revealing it.
    token_prefix    text NOT NULL,
    -- SHA-256 of the full token. Unique so a collision (or a re-issued identical
    -- token) is refused by the database, not merely by a lookup that could race.
    token_hash      bytea NOT NULL UNIQUE,
    -- The scope: the exact set of permissions this credential may exercise. A tool
    -- call is allowed only when its permission is in this list. Empty is refused by
    -- the application, so a credential always does something.
    permissions     text[] NOT NULL,
    -- NULL means the credential does not expire.
    expires_at      timestamptz,
    -- Soft revocation: a revoked credential is kept for the audit trail rather than
    -- deleted, and the lookup filters it out. This is the same posture the rest of
    -- the platform takes towards credentials.
    revoked_at      timestamptz,
    last_used_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER mcp_credentials_updated_at BEFORE UPDATE ON mcp_credentials
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Two credentials of one user may share a name; the lookup is by hash, not name.
-- The index that matters is the hash (UNIQUE above) and the per-owner listing.
CREATE INDEX mcp_credentials_owner_idx ON mcp_credentials (organization_id, owner_user_id, created_at DESC);

-- The retention sweep deletes by created_at across all organizations, so it needs
-- an index that leads with that column. The per-owner index leads with
-- organization_id and cannot serve it.
CREATE INDEX mcp_credentials_created_at_idx ON mcp_credentials (created_at);

-- Permission catalog. Reading credentials is separated from creating and revoking
-- them: listing what exists is an audit activity, and minting or revoking a
-- credential changes who can reach the CRM, which is its own right.
INSERT INTO permissions (key, category, description) VALUES
('mcp.credentials.read',   'mcp', 'View MCP client credentials'),
('mcp.credentials.manage', 'mcp', 'Create, revoke and rotate MCP client credentials')
ON CONFLICT (key) DO NOTHING;

-- mcp.credentials.read: the roles that administer the platform or the tenant may
-- see which credentials exist. It is not granted to every user: a credential list
-- names the users who have connected external clients.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'mcp.credentials.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin', 'security_auditor')
ON CONFLICT DO NOTHING;

-- mcp.credentials.manage: minting a credential delegates authority, so only the
-- administrators who already manage access may do it.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'mcp.credentials.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin')
ON CONFLICT DO NOTHING;
