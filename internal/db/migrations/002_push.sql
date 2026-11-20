CREATE TABLE push_subscriptions (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT    NOT NULL UNIQUE,
    p256dh     TEXT    NOT NULL,
    auth       TEXT    NOT NULL,
    device     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    last_ok_at INTEGER NOT NULL DEFAULT 0,
    fail_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX push_subscriptions_user ON push_subscriptions(user_id);

-- Digest schedule per user. times: JSON array of "HH:MM" (local time).
CREATE TABLE notify_settings (
    user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled       INTEGER NOT NULL DEFAULT 1,
    times         TEXT    NOT NULL DEFAULT '["08:00","13:00","20:00"]',
    quiet_enabled INTEGER NOT NULL DEFAULT 0,
    quiet_start   TEXT    NOT NULL DEFAULT '22:00',
    quiet_end     TEXT    NOT NULL DEFAULT '07:00',
    last_slot     TEXT    NOT NULL DEFAULT ''
);
