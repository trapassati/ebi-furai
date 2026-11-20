CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    disabled      INTEGER NOT NULL DEFAULT 0,
    prefs         TEXT    NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    user_agent TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE folders (
    id       INTEGER PRIMARY KEY,
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, name)
);

CREATE TABLE feeds (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_id       INTEGER REFERENCES folders(id) ON DELETE SET NULL,
    url             TEXT    NOT NULL,
    site_url        TEXT    NOT NULL DEFAULT '',
    title           TEXT    NOT NULL DEFAULT '',
    custom_title    TEXT    NOT NULL DEFAULT '',
    icon            BLOB,
    icon_type       TEXT    NOT NULL DEFAULT '',
    icon_checked_at INTEGER NOT NULL DEFAULT 0,
    etag            TEXT    NOT NULL DEFAULT '',
    last_modified   TEXT    NOT NULL DEFAULT '',
    last_fetched_at INTEGER NOT NULL DEFAULT 0,
    next_fetch_at   INTEGER NOT NULL DEFAULT 0,
    error_count     INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT    NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    UNIQUE (user_id, url)
);
CREATE INDEX feeds_next_fetch ON feeds(next_fetch_at);
CREATE INDEX feeds_user ON feeds(user_id);

CREATE TABLE articles (
    id           INTEGER PRIMARY KEY,
    feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL,
    guid_hash    TEXT    NOT NULL,
    url          TEXT    NOT NULL DEFAULT '',
    title        TEXT    NOT NULL DEFAULT '',
    author       TEXT    NOT NULL DEFAULT '',
    content      TEXT    NOT NULL DEFAULT '',
    full_content TEXT    NOT NULL DEFAULT '',
    preview      TEXT    NOT NULL DEFAULT '',
    thumbnail    TEXT    NOT NULL DEFAULT '',
    published_at INTEGER NOT NULL,
    fetched_at   INTEGER NOT NULL,
    read         INTEGER NOT NULL DEFAULT 0,
    starred      INTEGER NOT NULL DEFAULT 0,
    starred_at   INTEGER NOT NULL DEFAULT 0,
    UNIQUE (feed_id, guid_hash)
);
CREATE INDEX articles_user_pub    ON articles(user_id, published_at);
CREATE INDEX articles_user_unread ON articles(user_id, read, published_at);
CREATE INDEX articles_feed_pub    ON articles(feed_id, published_at);
CREATE INDEX articles_starred     ON articles(user_id, starred_at) WHERE starred = 1;

-- Contentless full-text index; rowid = articles.id.
CREATE VIRTUAL TABLE articles_fts USING fts5(
    title, body,
    content = '',
    contentless_delete = 1,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER articles_fts_delete AFTER DELETE ON articles BEGIN
    DELETE FROM articles_fts WHERE rowid = old.id;
END;

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
