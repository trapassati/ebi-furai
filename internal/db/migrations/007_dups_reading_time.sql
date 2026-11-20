-- Reading time: words of the article (the longest of feed text and full
-- text). NULL = not counted yet: the server counts the old ones at start.
ALTER TABLE articles ADD COLUMN words INTEGER;

-- Duplicates: the same article from two feeds (a newspaper and Google
-- News). Keys to find them; dup_of points to the first copy that arrived.
ALTER TABLE articles ADD COLUMN url_key TEXT NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN title_key TEXT NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN title_key2 TEXT NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN dup_of INTEGER;
CREATE INDEX articles_url_key ON articles(user_id, url_key) WHERE url_key <> '';
CREATE INDEX articles_title_key ON articles(user_id, title_key) WHERE title_key <> '';
CREATE INDEX articles_title_key2 ON articles(user_id, title_key2) WHERE title_key2 <> '';
CREATE INDEX articles_dup_of ON articles(dup_of) WHERE dup_of IS NOT NULL;
CREATE INDEX articles_uncounted ON articles(id) WHERE words IS NULL;

ALTER TABLE users ADD COLUMN dedup INTEGER NOT NULL DEFAULT 1;
