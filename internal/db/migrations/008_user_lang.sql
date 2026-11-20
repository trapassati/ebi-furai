-- The language of the user's last session, for messages sent without a
-- request (notifications).
ALTER TABLE users ADD COLUMN lang TEXT NOT NULL DEFAULT 'en';
