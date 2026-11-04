-- One-time codes are kept separately from links. In particular, pending codes
-- must not reserve the same globally unique telegram_user_id sentinel (0).
CREATE TABLE telegram_link_codes (
    code_hash bytea PRIMARY KEY CHECK (octet_length(code_hash) = 32),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX telegram_link_codes_user_idx ON telegram_link_codes (organization_id, user_id);
CREATE INDEX telegram_link_codes_expiry_idx ON telegram_link_codes (expires_at);
