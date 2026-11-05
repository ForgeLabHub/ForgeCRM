DROP INDEX IF EXISTS user_identities_provider_user_key;
ALTER TABLE oidc_login_states DROP COLUMN link_user_id;
