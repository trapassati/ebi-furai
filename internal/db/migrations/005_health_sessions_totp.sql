-- Feed health: when a feed was last fetched successfully.
ALTER TABLE feeds ADD COLUMN last_ok_at INTEGER NOT NULL DEFAULT 0;
UPDATE feeds SET last_ok_at = last_fetched_at WHERE error_count = 0;

-- Connected devices: the address a session was last seen from.
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';

-- Two-step verification (TOTP, RFC 6238). The secret is sealed with
-- AES-256-GCM like the feed passwords; totp_last_step stops a code from
-- being used twice.
ALTER TABLE users ADD COLUMN totp_secret BLOB;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;

-- One-time recovery codes for a lost phone (stored as SHA-256 hashes).
CREATE TABLE recovery_codes (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT    NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);
