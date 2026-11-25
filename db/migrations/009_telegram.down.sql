DROP TABLE telegram_login_proofs;
DROP TABLE telegram_link_codes;
ALTER TABLE users DROP COLUMN telegram_id, DROP COLUMN telegram_username,
    DROP COLUMN notifications_enabled, DROP COLUMN notification_preferences;
