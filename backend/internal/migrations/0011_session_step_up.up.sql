-- 0011_session_step_up: record when a session last presented a second factor.
--
-- The destructive tier of the agent approval model (ADR 0005) requires more than
-- an authenticated session: it requires that the session has presented a second
-- factor recently. `mfa_verified` cannot express this. It is set once, at login,
-- and then stays true for the whole session, so a session left open for twelve
-- hours would satisfy "a second factor" on the strength of a factor presented at
-- its start. What the tier needs is recency, which needs its own column.
--
-- The column is nullable and its absence means "no recent second factor", which
-- is the safe default: a session created before this migration, or one that never
-- stepped up, is refused for a destructive decision rather than assumed verified.
--
-- For a user with MFA enabled, login already presents a factor, so the step-up is
-- satisfied from the start and is refreshed by a fresh verification. For a user
-- without MFA, the step-up endpoint re-verifies the password, which is the factor
-- they have.

ALTER TABLE sessions ADD COLUMN step_up_at timestamptz;
