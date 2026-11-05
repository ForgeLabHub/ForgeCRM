-- Per-Telegram-account notification preferences, one row per (account, kind)
-- that the person explicitly turned off. A missing row means the default, which
-- is "enabled": the absence of a decision is not a refusal, so a new account
-- receives the notifications it is entitled to without any setup.
--
-- This is a preferences table, not a queue and not a second consent store. The
-- account-level consent gate (0027) still applies to every send; a preference can
-- only silence a type the person was otherwise entitled to receive. The key is the
-- Telegram account, like telegram_user_settings (0034), so a preference survives
-- an unlink/relink and can be set before linking.
CREATE TABLE telegram_notification_preferences (
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    kind             text NOT NULL CHECK (kind IN ('assignment', 'due_changed', 'overdue', 'completed')),
    enabled          boolean NOT NULL,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (telegram_user_id, kind)
);
