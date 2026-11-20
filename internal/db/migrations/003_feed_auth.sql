-- HTTP Basic credentials for feeds behind a password.
-- auth_secret is the password sealed with AES-256-GCM (key: setting feed_auth_key).
ALTER TABLE feeds ADD COLUMN auth_user TEXT NOT NULL DEFAULT '';
ALTER TABLE feeds ADD COLUMN auth_secret BLOB;
