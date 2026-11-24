package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/trapassati/ebi-furai/internal/filter"
)

const MaxFilters = 100

var ErrTooManyFilters = errors.New("you already have 100 filters: delete some")

// FilterInfo is a filter as the settings show it.
type FilterInfo struct {
	ID        int64    `json:"id"`
	Terms     []string `json:"terms"`
	Action    string   `json:"action"`
	FeedID    int64    `json:"feed_id,omitempty"`
	FolderID  int64    `json:"folder_id,omitempty"`
	InBody    bool     `json:"in_body"`
	Hidden    int      `json:"hidden"` // articles it hides now
	CreatedAt int64    `json:"created_at"`
}

func (f *FilterInfo) Rule() *filter.Rule {
	return &filter.Rule{ID: f.ID, Terms: f.Terms, Action: f.Action, FeedID: f.FeedID, FolderID: f.FolderID, InBody: f.InBody}
}

func nullID(id int64) any {
	if id > 0 {
		return id
	}
	return nil
}

// Filters returns a user's filters, newest first.
func (s *Store) Filters(userID int64) ([]*FilterInfo, error) {
	rows, err := s.DB.Query(`SELECT f.id, f.terms, f.action, COALESCE(f.feed_id, 0), COALESCE(f.folder_id, 0), f.in_body, f.created_at,
		(SELECT COUNT(*) FROM articles a WHERE a.hidden_by = f.id)
		FROM filters f WHERE f.user_id = ? ORDER BY f.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*FilterInfo{}
	for rows.Next() {
		f := &FilterInfo{}
		var terms string
		if err := rows.Scan(&f.ID, &terms, &f.Action, &f.FeedID, &f.FolderID, &f.InBody, &f.CreatedAt, &f.Hidden); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(terms), &f.Terms) != nil {
			f.Terms = []string{}
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

// FilterRules returns the rules the fetcher applies to a user's new articles.
func (s *Store) FilterRules(userID int64) ([]*filter.Rule, error) {
	list, err := s.Filters(userID)
	if err != nil {
		return nil, err
	}
	rules := make([]*filter.Rule, len(list))
	for i, f := range list {
		rules[i] = f.Rule()
	}
	return rules, nil
}

// CreateFilter stores a filter; the scope (feed or folder) must belong to
// the user.
func (s *Store) CreateFilter(userID int64, f *FilterInfo) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM filters WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return err
	}
	if n >= MaxFilters {
		return ErrTooManyFilters
	}
	if f.FeedID > 0 && !s.feedExists(userID, f.FeedID) {
		return ErrNotFound
	}
	if f.FolderID > 0 && !s.FolderExists(userID, f.FolderID) {
		return ErrNotFound
	}
	terms, _ := json.Marshal(f.Terms)
	f.CreatedAt = now()
	res, err := s.DB.Exec(`INSERT INTO filters(user_id, terms, action, feed_id, folder_id, in_body, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		userID, string(terms), f.Action, nullID(f.FeedID), nullID(f.FolderID), f.InBody, f.CreatedAt)
	if err != nil {
		return err
	}
	f.ID, err = res.LastInsertId()
	return err
}

func (s *Store) feedExists(userID, id int64) bool {
	var x int
	return s.DB.QueryRow(`SELECT 1 FROM feeds WHERE id = ? AND user_id = ?`, id, userID).Scan(&x) == nil
}

// DeleteFilter removes a filter; the articles it hid come back as unread.
// It returns how many came back.
func (s *Store) DeleteFilter(userID, id int64) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := execOne(tx, `DELETE FROM filters WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`UPDATE articles SET hidden_by = NULL, read = 0 WHERE hidden_by = ? AND user_id = ?`, id, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}

// FilterCandidate is an unread article a new filter may catch.
type FilterCandidate struct {
	ID       int64
	FeedID   int64
	FolderID int64
	Title    string
	Content  string // HTML, only loaded when the body is searched too
}

// FilterCandidates returns the unread, visible, not starred articles of a
// user, in a scope (feedID or folderID, both 0 for all), newest first.
func (s *Store) FilterCandidates(userID, feedID, folderID int64, withBody bool) ([]FilterCandidate, error) {
	body := "''"
	if withBody {
		body = "a.content"
	}
	q := `SELECT a.id, a.feed_id, COALESCE(f.folder_id, 0), a.title, ` + body + `
		FROM articles a JOIN feeds f ON f.id = a.feed_id
		WHERE a.user_id = ? AND a.read = 0 AND a.starred = 0 AND a.hidden_by IS NULL AND a.saved = 0`
	args := []any{userID}
	switch {
	case feedID > 0:
		q += ` AND a.feed_id = ?`
		args = append(args, feedID)
	case folderID > 0:
		q += ` AND f.folder_id = ?`
		args = append(args, folderID)
	}
	rows, err := s.DB.Query(q+` ORDER BY a.published_at DESC, a.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []FilterCandidate
	for rows.Next() {
		var c FilterCandidate
		if err := rows.Scan(&c.ID, &c.FeedID, &c.FolderID, &c.Title, &c.Content); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// HideArticles hides unread articles with a filter (they also become read).
// It returns the ids actually hidden.
func (s *Store) HideArticles(userID, filterID int64, ids []int64) ([]int64, error) {
	return s.updateIDs(`UPDATE articles SET hidden_by = ?, read = 1 WHERE user_id = ? AND read = 0 AND hidden_by IS NULL AND id IN (%s) RETURNING id`,
		[]any{filterID, userID}, ids)
}

// MarkReadIDs marks unread articles as read and returns the ids changed
// (for the undo).
func (s *Store) MarkReadIDs(userID int64, ids []int64) ([]int64, error) {
	return s.updateIDs(`UPDATE articles SET read = 1 WHERE user_id = ? AND read = 0 AND id IN (%s) RETURNING id`, []any{userID}, ids)
}

// updateIDs runs an UPDATE ... RETURNING id over ids in chunks, in one
// transaction. q has a %s for the placeholders.
func (s *Store) updateIDs(q string, head []any, ids []int64) ([]int64, error) {
	out := []int64{}
	if len(ids) == 0 {
		return out, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		args := append([]any{}, head...)
		marks := make([]string, len(chunk))
		for i, id := range chunk {
			marks[i] = "?"
			args = append(args, id)
		}
		rows, err := tx.Query(strings.Replace(q, "%s", strings.Join(marks, ","), 1), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

// ---- background full text ----

// PendingFullText is an article waiting for its full text.
type PendingFullText struct {
	ID  int64
	URL string
}

// PendingFullTexts returns the newest articles waiting for the background
// download (of enabled users, fetched in the last days).
func (s *Store) PendingFullTexts(since int64, limit int) ([]PendingFullText, error) {
	rows, err := s.DB.Query(`SELECT a.id, a.url FROM articles a
		WHERE a.full_tried = 0 AND a.fetched_at >= ?
		AND EXISTS (SELECT 1 FROM users u WHERE u.id = a.user_id AND u.disabled = 0)
		ORDER BY a.id DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []PendingFullText
	for rows.Next() {
		var p PendingFullText
		if err := rows.Scan(&p.ID, &p.URL); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// FullTextDone records the result of a background download (html empty
// when it failed: the reader keeps the feed's text); text, its plain text,
// goes into the search index.
func (s *Store) FullTextDone(id int64, html, text string, words int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE articles SET full_tried = 1, full_content = CASE WHEN ?1 <> '' THEN ?1 ELSE full_content END,
		words = MAX(COALESCE(words, 0), ?2) WHERE id = ?3`, html, words, id); err != nil {
		return err
	}
	if html != "" {
		if err := indexFullText(tx, id, text); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return tx.Commit()
}

// ExpireFullTexts gives up on articles that waited too long (the feed was
// turned off, the server was down for days).
func (s *Store) ExpireFullTexts(before int64) error {
	_, err := s.DB.Exec(`UPDATE articles SET full_tried = 1 WHERE full_tried = 0 AND fetched_at < ?`, before)
	return err
}
