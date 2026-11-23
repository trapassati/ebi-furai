package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
)

// "Read later": pages saved by the user from the browser (bookmarklet),
// the iPhone's share sheet (Shortcuts) or the app. They are articles of a
// hidden feed of the user, of kind 'saved', which is never fetched and is
// not listed among the feeds; the articles carry saved = 1.

const savedFeedURL = "ebi-furai:saved"

// SavedPage is what is kept of a saved page.
type SavedPage struct {
	URL       string
	Title     string
	Author    string
	Content   string // sanitized HTML, may be empty
	Preview   string
	Thumbnail string
	Text      string // plain text, for search and the reading time
	Words     int
}

// savedFeed returns the id of the user's "Read later" feed, creating it.
// Its title follows the language of the user at the time.
func savedFeed(tx *sql.Tx, userID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM feeds WHERE user_id = ? AND kind = 'saved'`, userID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var lang string
	_ = tx.QueryRow(`SELECT lang FROM users WHERE id = ?`, userID).Scan(&lang)
	title := "Read later"
	if lang == "it" {
		title = "Da leggere"
	}
	res, err := tx.Exec(`INSERT INTO feeds(user_id, url, title, kind, next_fetch_at, created_at) VALUES(?, ?, ?, 'saved', ?, ?)`,
		userID, savedFeedURL, title, int64(math.MaxInt64), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SavePage stores a page in "Read later" and returns its article id. The
// same address saved again comes back to the top as unread, keeping what
// was already there when the new copy could not be read.
func (s *Store) SavePage(userID int64, p SavedPage) (id int64, again bool, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	feedID, err := savedFeed(tx, userID)
	if err != nil {
		return 0, false, err
	}
	sum := sha256.Sum256([]byte(p.URL))
	guid := hex.EncodeToString(sum[:])
	t := now()
	err = tx.QueryRow(`SELECT id FROM articles WHERE feed_id = ? AND guid_hash = ?`, feedID, guid).Scan(&id)
	switch {
	case err == nil:
		again = true
		if p.Content != "" {
			_, err = tx.Exec(`UPDATE articles SET title = ?, author = ?, content = ?, preview = ?, thumbnail = ?, words = ?,
				published_at = ?, read = 0 WHERE id = ?`, p.Title, p.Author, p.Content, p.Preview, p.Thumbnail, p.Words, t, id)
			if err == nil {
				_, err = tx.Exec(`DELETE FROM articles_fts WHERE rowid = ?`, id)
			}
			if err == nil {
				_, err = tx.Exec(`INSERT INTO articles_fts(rowid, title, body) VALUES(?, ?, ?)`, id, p.Title, p.Text)
			}
		} else {
			_, err = tx.Exec(`UPDATE articles SET published_at = ?, read = 0 WHERE id = ?`, t, id)
		}
		if err != nil {
			return 0, false, err
		}
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec(`INSERT INTO articles(feed_id, user_id, guid_hash, url, title, author, content, preview, thumbnail,
			published_at, fetched_at, full_tried, words, saved) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, 1)`,
			feedID, userID, guid, p.URL, p.Title, p.Author, p.Content, p.Preview, p.Thumbnail, t, t, p.Words)
		if err != nil {
			return 0, false, err
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, false, err
		}
		if _, err := tx.Exec(`INSERT INTO articles_fts(rowid, title, body) VALUES(?, ?, ?)`, id, p.Title, p.Text); err != nil {
			return 0, false, err
		}
	default:
		return 0, false, err
	}
	return id, again, tx.Commit()
}

// DeleteSaved removes a page from "Read later".
func (s *Store) DeleteSaved(userID, id int64) error {
	return execOne(s.DB, `DELETE FROM articles WHERE id = ? AND user_id = ? AND saved = 1`, id, userID)
}
