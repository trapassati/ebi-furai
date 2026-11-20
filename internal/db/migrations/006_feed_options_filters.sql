-- Per-feed options: download the full text of new articles in background,
-- and open articles on the site instead of the reader.
ALTER TABLE feeds ADD COLUMN full_text INTEGER NOT NULL DEFAULT 0;
ALTER TABLE feeds ADD COLUMN open_web INTEGER NOT NULL DEFAULT 0;

-- full_tried: the background full-text download already ran (or is not
-- wanted). Only articles still waiting stay in the small partial index.
ALTER TABLE articles ADD COLUMN full_tried INTEGER NOT NULL DEFAULT 1;
CREATE INDEX articles_full_pending ON articles(id) WHERE full_tried = 0;

-- Keyword filters. terms: JSON array of normalized words or phrases (a
-- trailing * matches words starting with it). action: 'hide' or 'read'.
-- Scope: a feed, a folder, or every feed when both are NULL.
CREATE TABLE filters (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    terms      TEXT    NOT NULL,
    action     TEXT    NOT NULL CHECK (action IN ('hide', 'read')),
    feed_id    INTEGER REFERENCES feeds(id) ON DELETE CASCADE,
    folder_id  INTEGER REFERENCES folders(id) ON DELETE CASCADE,
    in_body    INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX filters_user ON filters(user_id);

-- Articles hidden by a filter: they are also read, and come back as unread
-- when the filter is deleted.
ALTER TABLE articles ADD COLUMN hidden_by INTEGER;
CREATE INDEX articles_hidden ON articles(hidden_by) WHERE hidden_by IS NOT NULL;
