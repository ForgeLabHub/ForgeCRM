-- Losing this table only forgets which language each Telegram account chose and
-- whether the introduction was shown; it holds no message content, link code or
-- CRM data, so a rollback is safe at any time.
DROP TABLE IF EXISTS telegram_user_settings;
