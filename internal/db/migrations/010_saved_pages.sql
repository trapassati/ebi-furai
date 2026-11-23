-- "Read later": pages saved from the browser or the iPhone's share sheet.
-- They live in a hidden feed of each user (kind 'saved'), which is never
-- fetched, and are flagged on the article so that the Unread, Today and All
-- collections can leave them out without a join.
ALTER TABLE feeds ADD COLUMN kind TEXT NOT NULL DEFAULT 'feed';
ALTER TABLE articles ADD COLUMN saved INTEGER NOT NULL DEFAULT 0;
CREATE INDEX articles_saved ON articles(user_id, read, published_at) WHERE saved = 1;
