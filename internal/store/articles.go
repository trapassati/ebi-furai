package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ErrBadCursor is a page cursor the client sent that cannot be read.
var ErrBadCursor = errors.New("invalid cursor")

type ArticleSummary struct {
	ID          int64  `json:"id"`
	FeedID      int64  `json:"feed_id"`
	FeedTitle   string `json:"feed_title"`
	FeedHasIcon bool   `json:"feed_has_icon"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Preview     string `json:"preview"`
	Thumbnail   string `json:"thumbnail"`
	PublishedAt int64  `json:"published_at"`
	Read        bool   `json:"read"`
	Starred     bool   `json:"starred"`
	Words       int    `json:"words,omitempty"` // for the reading time
	Saved       bool   `json:"saved,omitempty"` // in "Read later"
}

type Article struct {
	ArticleSummary
	Author      string `json:"author"`
	Content     string `json:"content"`
	FullContent string `json:"full_content,omitempty"`
	HasFull     bool   `json:"has_full"`
	SiteURL     string `json:"site_url"`
}

// Selection identifies a set of articles: a smart feed, a folder or a feed.
type Selection struct {
	UserID     int64
	Kind       string // today, unread, starred, all, saved, feed, folder
	ID         int64  // feed or folder id
	TodayStart int64
	Search     string
}

// ParseSource parses "today", "unread", "starred", "all", "saved", "feed:N", "folder:N".
func ParseSource(src string) (kind string, id int64, err error) {
	switch src {
	case "today", "unread", "starred", "all", "saved":
		return src, 0, nil
	}
	k, v, ok := strings.Cut(src, ":")
	if ok && (k == "feed" || k == "folder") {
		id, err = strconv.ParseInt(v, 10, 64)
		if err == nil && id > 0 {
			return k, id, nil
		}
	}
	return "", 0, errors.New("invalid source")
}

func (sel Selection) where() (string, []any) {
	conds := []string{"a.user_id = ?", "a.hidden_by IS NULL"}
	args := []any{sel.UserID}
	switch sel.Kind {
	case "today":
		conds = append(conds, "a.published_at >= ?")
		args = append(args, sel.TodayStart)
	case "unread":
		conds = append(conds, "a.read = 0")
	case "starred":
		conds = append(conds, "a.starred = 1")
	case "feed":
		conds = append(conds, "a.feed_id = ?")
		args = append(args, sel.ID)
	}
	// Pages saved for later have their own collection; the starred ones
	// are among the starred too.
	switch sel.Kind {
	case "saved":
		conds = append(conds, "a.saved = 1")
	case "today", "unread", "all", "folder":
		conds = append(conds, "a.saved = 0")
	}
	// A duplicate shows only in its own feed (or among the starred, if
	// starred there): everywhere else the first copy stands for both.
	if sel.Kind != "feed" && sel.Kind != "saved" {
		conds = append(conds, "(a.dup_of IS NULL OR a.starred = 1)")
	}
	switch sel.Kind {
	case "folder":
		conds = append(conds, "f.folder_id = ?")
		args = append(args, sel.ID)
	}
	if q := ftsQuery(sel.Search); q != "" {
		conds = append(conds, "a.id IN (SELECT rowid FROM articles_fts WHERE articles_fts MATCH ?)")
		args = append(args, q)
	}
	return strings.Join(conds, " AND "), args
}

// ftsQuery turns free text into a safe FTS5 query: every word becomes a
// quoted prefix term, all terms must match.
func ftsQuery(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var parts []string
	for _, w := range words {
		if len(parts) >= 8 {
			break
		}
		parts = append(parts, `"`+w+`"*`)
	}
	return strings.Join(parts, " ")
}

type ListOptions struct {
	UnreadOnly  bool
	StarredOnly bool
	Oldest      bool
	Cursor      string // "<published_at>:<id>"
	Limit       int
}

const summaryCols = `a.id, a.feed_id, COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url), f.icon IS NOT NULL,
	a.title, a.url, a.preview, a.thumbnail, a.published_at, a.read, a.starred, COALESCE(a.words, 0), a.saved`

func scanSummary(sc interface{ Scan(...any) error }, a *ArticleSummary) error {
	return sc.Scan(&a.ID, &a.FeedID, &a.FeedTitle, &a.FeedHasIcon, &a.Title, &a.URL, &a.Preview, &a.Thumbnail,
		&a.PublishedAt, &a.Read, &a.Starred, &a.Words, &a.Saved)
}

// ListArticles returns a page of articles and the cursor for the next one.
func (s *Store) ListArticles(sel Selection, opt ListOptions) ([]*ArticleSummary, string, error) {
	where, args := sel.where()
	if opt.UnreadOnly && sel.Kind != "starred" {
		where += " AND a.read = 0"
	}
	if opt.StarredOnly {
		where += " AND a.starred = 1"
	}
	if opt.Cursor != "" {
		p, i, ok := strings.Cut(opt.Cursor, ":")
		pub, err1 := strconv.ParseInt(p, 10, 64)
		id, err2 := strconv.ParseInt(i, 10, 64)
		if !ok || err1 != nil || err2 != nil {
			return nil, "", ErrBadCursor
		}
		if opt.Oldest {
			where += " AND (a.published_at > ? OR (a.published_at = ? AND a.id > ?))"
		} else {
			where += " AND (a.published_at < ? OR (a.published_at = ? AND a.id < ?))"
		}
		args = append(args, pub, pub, id)
	}
	order := "a.published_at DESC, a.id DESC"
	if opt.Oldest {
		order = "a.published_at ASC, a.id ASC"
	}
	if opt.Limit <= 0 || opt.Limit > 100 {
		opt.Limit = 50
	}
	q := fmt.Sprintf(`SELECT %s FROM articles a JOIN feeds f ON f.id = a.feed_id WHERE %s ORDER BY %s LIMIT %d`,
		summaryCols, where, order, opt.Limit+1)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	list := []*ArticleSummary{}
	for rows.Next() {
		a := &ArticleSummary{}
		if err := scanSummary(rows, a); err != nil {
			return nil, "", err
		}
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > opt.Limit {
		list = list[:opt.Limit]
		last := list[len(list)-1]
		next = fmt.Sprintf("%d:%d", last.PublishedAt, last.ID)
	}
	return list, next, nil
}

// MaxArticleID is used as an upper bound for "mark all as read".
func (s *Store) MaxArticleID(userID int64) int64 {
	var id sql.NullInt64
	s.DB.QueryRow(`SELECT MAX(id) FROM articles WHERE user_id = ?`, userID).Scan(&id)
	return id.Int64
}

// MarkAllRead marks the selection as read up to maxID (inclusive) and
// returns the articles it changed (those that were unread), so that the
// operation can be undone without touching the ones read before.
func (s *Store) MarkAllRead(sel Selection, maxID int64) ([]int64, error) {
	where, args := sel.where()
	args = append(args, maxID)
	rows, err := s.DB.Query(`UPDATE articles SET read = 1 WHERE id IN (
		SELECT a.id FROM articles a JOIN feeds f ON f.id = a.feed_id WHERE `+where+` AND a.read = 0 AND a.id <= ?)
		RETURNING id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkRange marks as read the unread articles of a list before or after
// an article, in the order the list shows them: "above" are the ones shown
// before it, "below" the ones after. The article itself is left alone. It
// returns the ids it changed (for the undo).
func (s *Store) MarkRange(sel Selection, opt ListOptions, pivotPub, pivotID int64, above bool, maxID int64) ([]int64, error) {
	where, args := sel.where()
	if opt.StarredOnly {
		where += " AND a.starred = 1"
	}
	// Newest first: above = newer. Oldest first: above = older.
	newer := above != opt.Oldest
	if newer {
		where += " AND (a.published_at > ? OR (a.published_at = ? AND a.id > ?))"
	} else {
		where += " AND (a.published_at < ? OR (a.published_at = ? AND a.id < ?))"
	}
	args = append(args, pivotPub, pivotPub, pivotID, maxID)
	rows, err := s.DB.Query(`UPDATE articles SET read = 1 WHERE id IN (
		SELECT a.id FROM articles a JOIN feeds f ON f.id = a.feed_id WHERE `+where+` AND a.read = 0 AND a.id <= ?)
		RETURNING id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkUnread marks articles of a user as unread again (undo of MarkAllRead).
func (s *Store) MarkUnread(userID int64, ids []int64) (int64, error) {
	var total int64
	for len(ids) > 0 {
		n := min(len(ids), 500)
		args := make([]any, 0, n+1)
		args = append(args, userID)
		marks := make([]string, n)
		for i, id := range ids[:n] {
			marks[i] = "?"
			args = append(args, id)
		}
		res, err := s.DB.Exec(`UPDATE articles SET read = 0 WHERE user_id = ? AND id IN (`+strings.Join(marks, ",")+`)`, args...)
		if err != nil {
			return total, err
		}
		c, _ := res.RowsAffected()
		total += c
		ids = ids[n:]
	}
	return total, nil
}

func (s *Store) GetArticle(userID, id int64) (*Article, error) {
	a := &Article{}
	row := s.DB.QueryRow(`SELECT `+summaryCols+`, a.author, a.content, a.full_content, f.site_url
		FROM articles a JOIN feeds f ON f.id = a.feed_id WHERE a.id = ? AND a.user_id = ?`, id, userID)
	err := row.Scan(&a.ID, &a.FeedID, &a.FeedTitle, &a.FeedHasIcon, &a.Title, &a.URL, &a.Preview, &a.Thumbnail,
		&a.PublishedAt, &a.Read, &a.Starred, &a.Words, &a.Saved, &a.Author, &a.Content, &a.FullContent, &a.SiteURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	a.HasFull = a.FullContent != ""
	return a, err
}

func (s *Store) SetRead(userID, id int64, read bool) error {
	return execOne(s.DB, `UPDATE articles SET read = ? WHERE id = ? AND user_id = ?`, read, id, userID)
}

// ReadDuplicates marks as read the other copies of an article (same story
// from another feed) and returns their ids.
func (s *Store) ReadDuplicates(userID, id int64) ([]int64, error) {
	rows, err := s.DB.Query(`UPDATE articles SET read = 1
		WHERE user_id = ?1 AND read = 0 AND id <> ?2
		AND (SELECT COALESCE(o.dup_of, o.id) FROM articles o WHERE o.id = ?2 AND o.user_id = ?1) IN (id, dup_of)
		RETURNING id`, userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			return nil, err
		}
		ids = append(ids, x)
	}
	return ids, rows.Err()
}

func (s *Store) SetStarred(userID, id int64, starred bool) error {
	return execOne(s.DB, `UPDATE articles SET starred = ?, starred_at = CASE WHEN ? THEN ? ELSE 0 END
		WHERE id = ? AND user_id = ?`, starred, starred, now(), id, userID)
}

// SetFullContent stores the full text and its words (the reading time
// follows the longer text).
func (s *Store) SetFullContent(userID, id int64, html string, words int) error {
	return execOne(s.DB, `UPDATE articles SET full_content = ?, full_tried = 1, words = MAX(COALESCE(words, 0), ?)
		WHERE id = ? AND user_id = ?`, html, words, id, userID)
}
