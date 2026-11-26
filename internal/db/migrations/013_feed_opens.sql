-- How many articles of each feed were opened, day by day (days since the
-- Unix epoch, UTC). Counted from now on, for the feed statistics: which
-- feeds are read and which are only marked as read or ignored. An article
-- counts once, when opening it makes it read.
CREATE TABLE feed_opens (
    feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    day     INTEGER NOT NULL,
    opens   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (feed_id, day)
) WITHOUT ROWID;
