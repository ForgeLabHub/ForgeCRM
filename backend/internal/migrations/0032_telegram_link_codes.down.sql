-- Codes are short-lived authentication challenges, not durable CRM records.
-- Rolling back invalidates outstanding challenges; existing links are retained.
DROP TABLE IF EXISTS telegram_link_codes;
