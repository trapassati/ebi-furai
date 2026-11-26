-- YouTube feeds: hide the Shorts (articles linked as youtube.com/shorts/…).
-- Hidden ones get hidden_by = -1 (no filter has that id) and are read, so
-- every list and counter already leaves them out.
ALTER TABLE feeds ADD COLUMN hide_shorts INTEGER NOT NULL DEFAULT 0;
