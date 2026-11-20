-- Feed refresh interval chosen by each user, in minutes (0 = server default, REFRESH_INTERVAL).
ALTER TABLE users ADD COLUMN refresh_minutes INTEGER NOT NULL DEFAULT 0;
