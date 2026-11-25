ALTER TABLE users
    ADD COLUMN telegram_id BIGINT UNIQUE CHECK (telegram_id > 0),
    ADD COLUMN telegram_username TEXT,
    ADD COLUMN notifications_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN notification_preferences JSONB NOT NULL DEFAULT '{"incidents":true,"work":true,"inventory":true,"attendance":true}';

CREATE TABLE telegram_link_codes (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    digest BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE telegram_login_proofs (
    digest BYTEA PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX telegram_login_proofs_expiry_idx ON telegram_login_proofs (expires_at);
