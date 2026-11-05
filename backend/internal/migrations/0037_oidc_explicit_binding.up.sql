-- 0037_oidc_explicit_binding: let a signed-in user bind a provider identity to
-- their own account, instead of the server guessing from a verified email.
--
-- Before this, a first sign-in that matched no existing link fell back to looking
-- the account up by the address the provider asserted, and attached the subject to
-- whatever local account carried that address. The provider owns the address, so a
-- provider that lets a user set an arbitrary *verified* address could take over an
-- account it does not own. The lookup is removed; binding is now a deliberate act
-- by the signed-in account holder.
--
-- oidc_login_states.link_user_id carries that intent through the redirect the
-- browser makes to the provider and back. It is nullable: NULL is an ordinary
-- sign-in, a non-NULL value is "bind the resulting subject to this account". The
-- value is set only by an authenticated, CSRF-checked request, and it is read on
-- the callback inside the same transaction that writes the link.
ALTER TABLE oidc_login_states
    ADD COLUMN link_user_id uuid REFERENCES users(id) ON DELETE CASCADE;

-- At most one identity per account per provider. Two links at one provider would
-- make "which account does this subject mean" ambiguous, and unlinking one would
-- leave the other silently in place. The service refuses the same case; the index
-- is what holds under two concurrent requests, where both can pass the check before
-- either writes.
CREATE UNIQUE INDEX user_identities_provider_user_key
    ON user_identities (provider_id, user_id);
