-- 0022_sso_oidc: corporate single sign-on over OpenID Connect.
--
-- Three tables.
--
-- An oidc_provider is one configured identity provider. It is scoped to an
-- organization, or to the whole platform when organization_id is NULL: a
-- corporate tenant configures its own Entra ID / Okta / Keycloak, and an
-- operator may configure one that every organization can use. The client secret
-- is stored only as ciphertext under the platform keyring, never in plaintext and
-- never in the environment once it has been set through the API.
--
-- A user_identity is the link between a local account and one subject at one
-- provider. The pair (provider, subject) is the provider's stable identifier for
-- a person — an email address is not, because people change addresses and
-- providers recycle them. Uniqueness is enforced on the pair, so a subject can
-- never be silently re-pointed at a second account.
--
-- An oidc_login_state is one in-flight authorization request. It carries the
-- anti-forgery state, the nonce that binds the ID token to this request and the
-- PKCE verifier. Rows are single-use and short-lived; the retention sweeper
-- removes expired ones.

CREATE TABLE oidc_providers (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULL means the provider is available platform-wide.
    organization_id         uuid REFERENCES organizations(id) ON DELETE CASCADE,
    key                     text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,62}$'),
    display_name            text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
    issuer                  text NOT NULL,
    client_id               text NOT NULL,
    -- Sealed with the platform keyring (AES-256-GCM). Empty is allowed: a public
    -- client using PKCE alone has no secret.
    client_secret_encrypted bytea,
    scopes                  text[] NOT NULL DEFAULT '{openid,email,profile}',
    -- Domains whose email addresses may be provisioned or matched. Empty means any
    -- address the provider asserts, which is only safe for a provider that already
    -- restricts who it authenticates.
    allowed_email_domains   text[] NOT NULL DEFAULT '{}',
    -- When true, a first-time sign-in creates a local account. When false, only an
    -- existing, already-linked account may sign in.
    jit_provisioning        boolean NOT NULL DEFAULT false,
    -- Role granted to a JIT-provisioned account. Ignored when the account already
    -- has a membership.
    default_role_key        text,
    enabled                 boolean NOT NULL DEFAULT true,
    created_by              uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER oidc_providers_updated_at BEFORE UPDATE ON oidc_providers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- The sentinel keeps uniqueness meaningful for both scopes.
CREATE UNIQUE INDEX oidc_providers_scope_key ON oidc_providers (
    COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid), key
);
CREATE INDEX oidc_providers_org_idx ON oidc_providers (organization_id, enabled);

CREATE TABLE user_identities (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id   uuid NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
    subject       text NOT NULL,
    email         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    -- One subject at one provider maps to exactly one local account.
    UNIQUE (provider_id, subject)
);
CREATE INDEX user_identities_user_idx ON user_identities (user_id);

CREATE TABLE oidc_login_states (
    state         text PRIMARY KEY,
    provider_id   uuid NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
    nonce         text NOT NULL,
    code_verifier text NOT NULL,
    -- Where to send the browser after a successful sign-in. Validated as a relative
    -- path before it is stored, so it cannot become an open redirect.
    return_to     text NOT NULL DEFAULT '/',
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz
);
-- The sweeper deletes by expiry; the index keeps that from scanning the table.
CREATE INDEX oidc_login_states_expires_idx ON oidc_login_states (expires_at);

-- Permission catalog entries, same shape as 0006/0013/0019/0020. The names are
-- added to the Go catalog (internal/rbac) in the same change as this migration,
-- and TestSeedDataMatchesThePermissionCatalog fails if the two disagree.
--
--   sso.read    view the configured identity providers.
--   sso.manage  create, change, enable and disable identity providers.
INSERT INTO permissions (key, category, description) VALUES
('sso.read',   'security', 'View the configured SSO identity providers'),
('sso.manage', 'security', 'Configure SSO identity providers')
ON CONFLICT (key) DO NOTHING;

-- sso.read: roles whose purpose includes looking at security configuration.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'sso.read'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin',
                'security_auditor', 'readonly_auditor')
ON CONFLICT DO NOTHING;

-- sso.manage: the roles that may change how people sign in.
INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, 'sso.manage'
FROM roles r
WHERE r.key IN ('platform_owner', 'platform_admin', 'tenant_admin')
ON CONFLICT DO NOTHING;
