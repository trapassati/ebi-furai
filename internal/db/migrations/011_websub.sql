-- WebSub (instant updates): one subscription per feed address, shared by
-- every user who follows it. The hub calls /websub/{token} as soon as the
-- feed changes, and the server fetches the feed right away; the content of
-- the notification itself is never used.
CREATE TABLE websub (
    id             INTEGER PRIMARY KEY,
    feed_url       TEXT    NOT NULL UNIQUE,        -- feeds.url
    hub            TEXT    NOT NULL,
    topic          TEXT    NOT NULL,               -- the feed's self address, as the hub knows it
    declared       INTEGER NOT NULL DEFAULT 1,     -- the feed still names a hub
    token          TEXT    NOT NULL UNIQUE,        -- in the callback address
    secret         TEXT    NOT NULL,               -- HMAC key of the notifications
    callback       TEXT    NOT NULL DEFAULT '',    -- the address given to the hub
    pending        TEXT    NOT NULL DEFAULT '',    -- 'subscribe' or 'unsubscribe' while waiting for the hub
    lease_seconds  INTEGER NOT NULL DEFAULT 0,
    verified_until INTEGER NOT NULL DEFAULT 0,     -- end of the lease: instant updates until then
    requested_at   INTEGER NOT NULL DEFAULT 0,
    notified_at    INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL
);

CREATE INDEX feeds_url ON feeds(url);
