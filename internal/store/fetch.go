package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/trapassati/ebi-furai/internal/dedup"
)

// FetchFeed holds what the fetcher needs to poll a feed.
type FetchFeed struct {
	ID             int64
	UserID         int64
	URL            string
	SiteURL        string
	Title          string
	ETag           string
	LastModified   string
	ErrorCount     int
	HasIcon        bool
	IconCheckedAt  int64
	LastFetchedAt  int64
	RefreshMinutes int    // the owner's interval (0 = server default)
	FolderID       int64  // 0 = no folder (for the filters)
	FullText       bool   // download the full text of new articles
	AuthUser       string // HTTP Basic credentials, empty when none
	AuthPass       string
	authSecret     []byte
}

const fetchCols = `id, user_id, url, site_url, title, etag, last_modified, error_count, icon IS NOT NULL, icon_checked_at, last_fetched_at, auth_user, auth_secret,
	(SELECT u.refresh_minutes FROM users u WHERE u.id = feeds.user_id), COALESCE(folder_id, 0), full_text`

func scanFetch(sc interface{ Scan(...any) error }) (*FetchFeed, error) {
	f := &FetchFeed{}
	err := sc.Scan(&f.ID, &f.UserID, &f.URL, &f.SiteURL, &f.Title, &f.ETag, &f.LastModified, &f.ErrorCount,
		&f.HasIcon, &f.IconCheckedAt, &f.LastFetchedAt, &f.AuthUser, &f.authSecret, &f.RefreshMinutes,
		&f.FolderID, &f.FullText)
	return f, err
}

func (s *Store) queryFetch(q string, args ...any) ([]*FetchFeed, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*FetchFeed
	for rows.Next() {
		f, err := scanFetch(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, f := range list {
		s.unsealFetch(f)
	}
	return list, nil
}

// unsealFetch decrypts the feed password for the fetcher. A password that
// cannot be read is left empty: the server will answer 401 and the feed
// shows the error, asking for the credentials again.
func (s *Store) unsealFetch(f *FetchFeed) {
	if f.AuthUser == "" {
		return
	}
	if p, err := s.openPassword(f.ID, f.authSecret); err == nil {
		f.AuthPass = p
	}
	f.authSecret = nil
}

// DueFeeds returns feeds whose next fetch time has passed.
func (s *Store) DueFeeds(limit int) ([]*FetchFeed, error) {
	return s.queryFetch(`SELECT `+fetchCols+` FROM feeds
		WHERE next_fetch_at <= ? AND kind = 'feed' AND EXISTS (SELECT 1 FROM users u WHERE u.id = feeds.user_id AND u.disabled = 0)
		ORDER BY next_fetch_at LIMIT ?`, now(), limit)
}

func (s *Store) UserFetchFeeds(userID int64) ([]*FetchFeed, error) {
	return s.queryFetch(`SELECT `+fetchCols+` FROM feeds WHERE user_id = ? AND kind = 'feed'`, userID)
}

func (s *Store) FetchFeedByID(userID, id int64) (*FetchFeed, error) {
	f, err := scanFetch(s.DB.QueryRow(`SELECT `+fetchCols+` FROM feeds WHERE id = ? AND user_id = ? AND kind = 'feed'`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.unsealFetch(f)
	return f, nil
}

// FeedFetched records a successful poll (also used for 304 Not Modified).
func (s *Store) FeedFetched(id int64, title, siteURL, etag, lastMod string, next int64) error {
	_, err := s.DB.Exec(`UPDATE feeds SET
		title = CASE WHEN ? <> '' THEN ? ELSE title END,
		site_url = CASE WHEN ? <> '' THEN ? ELSE site_url END,
		etag = ?, last_modified = ?, last_fetched_at = ?, last_ok_at = ?, next_fetch_at = ?,
		error_count = 0, last_error = ''
		WHERE id = ?`, title, title, siteURL, siteURL, etag, lastMod, now(), now(), next, id)
	return err
}

// FeedNotModified keeps validators and content, just reschedules.
func (s *Store) FeedNotModified(id int64, next int64) error {
	_, err := s.DB.Exec(`UPDATE feeds SET last_fetched_at = ?, last_ok_at = ?, next_fetch_at = ?, error_count = 0, last_error = '' WHERE id = ?`,
		now(), now(), next, id)
	return err
}

func (s *Store) FeedFailed(id int64, msg string, next int64) error {
	_, err := s.DB.Exec(`UPDATE feeds SET error_count = error_count + 1, last_error = ?, next_fetch_at = ?, last_fetched_at = ? WHERE id = ?`,
		truncate(msg, 300), next, now(), id)
	return err
}

func (s *Store) SetFeedIcon(id int64, icon []byte, typ string) error {
	var blob any
	if len(icon) > 0 {
		blob = icon
	}
	_, err := s.DB.Exec(`UPDATE feeds SET icon = COALESCE(?, icon), icon_type = CASE WHEN ? IS NULL THEN icon_type ELSE ? END,
		icon_checked_at = ? WHERE id = ?`, blob, blob, typ, now(), id)
	return err
}

// ScheduleNow makes the scheduler pick up a user's feeds right away.
func (s *Store) ScheduleNow(userID int64) error {
	_, err := s.DB.Exec(`UPDATE feeds SET next_fetch_at = 0 WHERE user_id = ?`, userID)
	return err
}

type NewArticle struct {
	GUIDHash    string
	URL         string
	Title       string
	Author      string
	Content     string
	Preview     string
	Thumbnail   string
	PublishedAt int64
	Text        string // plain text for the full-text index
	Read        bool   // a filter marked it as read
	HiddenBy    int64  // the filter that hides it, 0 = visible
}

// InsertArticles stores new items of a feed, ignoring duplicates. It
// returns the number of newly inserted articles. With fullText, the visible
// ones wait for the background full-text download.
func (s *Store) InsertArticles(feedID, userID int64, fullText bool, items []NewArticle) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var dedupOn bool
	if err := tx.QueryRow(`SELECT dedup FROM users WHERE id = ?`, userID).Scan(&dedupOn); err != nil {
		return 0, err
	}
	ins, err := tx.Prepare(`INSERT INTO articles(feed_id, user_id, guid_hash, url, title, author, content, preview, thumbnail,
		published_at, fetched_at, read, hidden_by, full_tried, words, url_key, title_key, title_key2, dup_of)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(feed_id, guid_hash) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer ins.Close()
	fts, err := tx.Prepare(`INSERT INTO articles_fts(rowid, title, body) VALUES(?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer fts.Close()
	t := now()
	added := 0
	for _, a := range items {
		var hidden any
		if a.HiddenBy > 0 {
			hidden = a.HiddenBy
		}
		urlKey := dedup.URLKey(a.URL)
		tk, tk2 := dedup.TitleKeys(a.Title)
		var dupOf any
		if dedupOn {
			id, err := findDuplicate(tx, userID, feedID, t-dupWindow, urlKey, tk, tk2)
			if err != nil {
				return 0, err
			}
			if id > 0 {
				// A copy is already there: this one arrives read, and
				// reading either marks both.
				dupOf = id
				a.Read = true
			}
		}
		waitFull := fullText && a.URL != "" && !a.Read
		res, err := ins.Exec(feedID, userID, a.GUIDHash, a.URL, a.Title, a.Author, a.Content, a.Preview, a.Thumbnail, a.PublishedAt, t,
			a.Read, hidden, !waitFull, dedup.Words(a.Text), urlKey, tk, tk2, dupOf)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		if _, err := fts.Exec(id, a.Title, a.Text); err != nil {
			return 0, err
		}
		added++
	}
	return added, tx.Commit()
}

// dupWindow: copies of an article arrive within hours of each other; two
// days leave room for slow feeds without matching old, reused titles.
const dupWindow = 2 * 24 * 3600

// findDuplicate returns the first copy (the group's head) of an article
// already stored by another feed of the user since a time, or 0.
func findDuplicate(tx *sql.Tx, userID, feedID, since int64, urlKey, tk, tk2 string) (int64, error) {
	var conds []string
	var args []any
	add := func(cond string, v string) {
		if v != "" {
			conds = append(conds, cond)
			args = append(args, v)
		}
	}
	add("url_key = ?", urlKey)
	add("title_key = ?", tk)
	add("title_key = ?", tk2)  // "Titolo - La Stampa" after "Titolo"
	add("title_key2 = ?", tk)  // "Titolo" after "Titolo - La Stampa"
	add("title_key2 = ?", tk2) // the same title from two aggregators
	if len(conds) == 0 {
		return 0, nil
	}
	var id int64
	err := tx.QueryRow(`SELECT COALESCE(dup_of, id) FROM articles
		WHERE user_id = ? AND feed_id <> ? AND fetched_at >= ? AND hidden_by IS NULL AND saved = 0 AND (`+strings.Join(conds, " OR ")+`)
		ORDER BY id LIMIT 1`, append([]any{userID, feedID, since}, args...)...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// Cleanup deletes articles older than cutoff (by publish and fetch time)
// except starred ones and the pages saved for later still to read.
// Returns the number of deleted rows.
func (s *Store) Cleanup(cutoff int64) (int64, error) {
	res, err := s.DB.Exec(`DELETE FROM articles WHERE starred = 0 AND (saved = 0 OR read = 1) AND published_at < ? AND fetched_at < ?`, cutoff, cutoff)
	if err != nil {
		return 0, err
	}
	if err := s.releaseOrphanDuplicates(); err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// releaseOrphanDuplicates: when the first copy is gone (deleted feed, old
// article), the others are on their own again.
func (s *Store) releaseOrphanDuplicates() error {
	_, err := s.DB.Exec(`UPDATE articles SET dup_of = NULL
		WHERE dup_of IS NOT NULL AND NOT EXISTS (SELECT 1 FROM articles o WHERE o.id = articles.dup_of)`)
	return err
}

// ---- words and duplicate keys of articles stored before they existed ----

// UncountedArticle is an article without words and keys yet.
type UncountedArticle struct {
	ID                            int64
	URL, Title, Content, FullText string
}

func (s *Store) UncountedArticles(limit int) ([]UncountedArticle, error) {
	rows, err := s.DB.Query(`SELECT id, url, title, content, full_content FROM articles WHERE words IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []UncountedArticle
	for rows.Next() {
		var a UncountedArticle
		if err := rows.Scan(&a.ID, &a.URL, &a.Title, &a.Content, &a.FullText); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// SetArticleCounts stores words and keys of an article (from UncountedArticles).
func (s *Store) SetArticleCounts(id int64, words int, urlKey, tk, tk2 string) error {
	_, err := s.DB.Exec(`UPDATE articles SET words = ?, url_key = ?, title_key = ?, title_key2 = ? WHERE id = ?`, words, urlKey, tk, tk2, id)
	return err
}

// KnownGUIDs returns the GUID hashes already stored for a feed.
func (s *Store) KnownGUIDs(feedID int64) (map[string]bool, error) {
	rows, err := s.DB.Query(`SELECT guid_hash FROM articles WHERE feed_id = ?`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		m[h] = true
	}
	return m, rows.Err()
}

// Optimize lets SQLite refresh its query planner statistics (cheap).
func (s *Store) Optimize() {
	s.DB.Exec(`PRAGMA optimize`)
}
