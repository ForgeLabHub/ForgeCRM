-- 0038_oidc_state_browser_binding: bind the authorization-code state to the
-- browser that began the flow, so a state captured elsewhere cannot be replayed
-- into a victim's session (login CSRF / session swapping).
--
-- The state was already random, single-use and expiring, but it was accepted from
-- any browser that presented it. An attacker could start a sign-in, keep the
-- authorization response, and hand the callback URL to a victim: the victim's
-- browser would then complete a sign-in the attacker initiated and land in the
-- attacker's account, silently. Comparing an HttpOnly cookie the browser must also
-- send closes that, because the cookie is never carried by the attacker's link.
--
-- The column stores the SHA-256 of the cookie value, never the value itself: a
-- read of this table must not yield a usable binding secret. It is nullable only
-- because the ALTER cannot add a NOT NULL column to a table with existing rows
-- without a default. The callback refuses a state whose stored hash is NULL, so a
-- state that was in flight across a rolling deploy is refused rather than completed
-- without the check; the browser simply starts the sign-in again. New states always
-- set it.
ALTER TABLE oidc_login_states
    ADD COLUMN browser_binding_hash text;
