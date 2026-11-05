-- Telegram assistant preferences, keyed by the Telegram account rather than the
-- CRM user. A person chooses a language before linking, and keeps it across an
-- unlink/relink; storing it here also lets an unlinked account be answered in the
-- language it picked. `onboarded_at` records that the first-run introduction was
-- shown, so /start does not repeat it on every message.
CREATE TABLE telegram_user_settings (
    telegram_user_id bigint PRIMARY KEY CHECK (telegram_user_id > 0),
    locale           text NOT NULL DEFAULT 'en' CHECK (locale IN ('en', 'ru')),
    onboarded_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
