-- 0024_transient_retention_indexes: indexes for the transient rows the sweep now
-- covers.
--
-- Three tables grow by one row per user action and were never pruned:
--
--   * oidc_login_states — one row per started OpenID Connect sign-in. Migration
--     0022 documented a sweeper that removes expired rows and created the index
--     for it, but the sweeper was never added; the retention registry now carries
--     the policy, so this migration only has to make sure the other two tables
--     have an index the sweep can use.
--   * password_reset_tokens — one row per requested password reset, single-use and
--     valid for an hour. It has an index on user_id but none on expires_at, which
--     is the column the sweep deletes by.
--   * email_log — one row per email the platform tried to send. The existing
--     email_log_status_idx leads with status, not created_at, so it cannot serve a
--     sweep bounded by time.
--
-- The sweep's eligibility rule for email_log is "settled", so the index is partial
-- and covers only sent or failed rows; a queued email is still pending and is never
-- in the index. password_reset_tokens is swept on expiry, so its index is plain.
--
-- CONCURRENTLY is not used: migrations run inside a transaction.

-- password_reset_tokens: an expired reset token can never be redeemed.
CREATE INDEX IF NOT EXISTS password_reset_tokens_expires_idx
    ON password_reset_tokens (expires_at);

-- email_log: only a sent or failed email is swept.
CREATE INDEX IF NOT EXISTS email_log_settled_idx
    ON email_log (created_at) WHERE status IN ('sent', 'failed');
