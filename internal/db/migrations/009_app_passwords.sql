-- App passwords: for apps that sync through the Google Reader API (Reeder,
-- NetNewsWire…), which cannot do two-step verification. Each is a random
-- secret shown once; only its SHA-256 is kept.
CREATE TABLE app_passwords (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    secret_hash  TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    last_ip      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX app_passwords_user ON app_passwords(user_id);

-- The tokens an app gets at sign-in (ClientLogin); they go with the app
-- password that made them.
CREATE TABLE api_tokens (
    token_hash      TEXT    PRIMARY KEY,
    app_password_id INTEGER NOT NULL REFERENCES app_passwords(id) ON DELETE CASCADE,
    created_at      INTEGER NOT NULL
);
CREATE INDEX api_tokens_password ON api_tokens(app_password_id);
