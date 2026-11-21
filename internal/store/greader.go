package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Queries for the Google Reader API (internal/api/greader.go), which apps
// like Reeder and NetNewsWire use to sync. The API sees the same articles
// as the lists of the app: not the ones hidden by a filter, and a
// duplicate only through its first copy.

// APIFeed is a subscription as the API lists it.
type APIFeed struct {
	ID      int64
	Title   string
	URL     string
	SiteURL string
	Folder  string // "" = no folder
}

// APIFeeds lists the user's feeds with the name of their folder.
func (s *Store) APIFeeds(userID int64) ([]APIFeed, error) {
	rows, err := s.DB.Query(`SELECT f.id, COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url), f.url, f.site_url,
		COALESCE(fo.name, '')
		FROM feeds f LEFT JOIN folders fo ON fo.id = f.folder_id
		WHERE f.user_id = ? ORDER BY COALESCE(fo.position, 0), lower(COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url))`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []APIFeed{}
	for rows.Next() {
		var f APIFeed
		if err := rows.Scan(&f.ID, &f.Title, &f.URL, &f.SiteURL, &f.Folder); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

// APIFolders lists the folder names of the user, in their order.
func (s *Store) APIFolders(userID int64) ([]string, error) {
	rows, err := s.DB.Query(`SELECT name FROM folders WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

// FolderIDByName returns the id of an existing folder.
func (s *Store) FolderIDByName(userID int64, name string) (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM folders WHERE user_id = ? AND name = ?`, userID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// FeedFolderID returns the folder of a feed (0 = none).
func (s *Store) FeedFolderID(userID, feedID int64) (int64, error) {
	var id sql.NullInt64
	err := s.DB.QueryRow(`SELECT folder_id FROM feeds WHERE id = ? AND user_id = ?`, feedID, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id.Int64, err
}

// FeedUnreadCount is the unread count of a feed and its newest article.
type FeedUnreadCount struct {
	FeedID int64
	Count  int
	Newest int64 // published_at of the newest unread article
}

// APIUnreadCounts returns the unread count of every feed that has some.
func (s *Store) APIUnreadCounts(userID int64) ([]FeedUnreadCount, error) {
	rows, err := s.DB.Query(`SELECT feed_id, COUNT(*), MAX(published_at) FROM articles
		WHERE user_id = ? AND read = 0 AND hidden_by IS NULL AND dup_of IS NULL GROUP BY feed_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []FeedUnreadCount{}
	for rows.Next() {
		var c FeedUnreadCount
		if err := rows.Scan(&c.FeedID, &c.Count, &c.Newest); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// APIStream is a set of articles asked for by the API.
type APIStream struct {
	UserID   int64
	FeedID   int64 // a feed
	FolderID int64 // a folder (label)
	Starred  bool  // only starred
	Read     bool  // only read
	Unread   bool  // only unread (exclude read)
	NoStar   bool  // exclude starred
	Since    int64 // published at or after (Unix seconds), 0 = any
	Until    int64 // published before, 0 = any
}

func (st APIStream) where() (string, []any) {
	conds := []string{"a.user_id = ?", "a.hidden_by IS NULL"}
	args := []any{st.UserID}
	if st.FeedID > 0 {
		conds = append(conds, "a.feed_id = ?")
		args = append(args, st.FeedID)
	} else {
		conds = append(conds, "(a.dup_of IS NULL OR a.starred = 1)")
	}
	if st.FolderID > 0 {
		conds = append(conds, "a.feed_id IN (SELECT id FROM feeds WHERE folder_id = ? AND user_id = ?)")
		args = append(args, st.FolderID, st.UserID)
	}
	if st.Starred {
		conds = append(conds, "a.starred = 1")
	}
	if st.NoStar {
		conds = append(conds, "a.starred = 0")
	}
	if st.Read {
		conds = append(conds, "a.read = 1")
	}
	if st.Unread {
		conds = append(conds, "a.read = 0")
	}
	if st.Since > 0 {
		conds = append(conds, "a.published_at >= ?")
		args = append(args, st.Since)
	}
	if st.Until > 0 {
		conds = append(conds, "a.published_at < ?")
		args = append(args, st.Until)
	}
	return strings.Join(conds, " AND "), args
}

// APIItemRef is an article id with its date.
type APIItemRef struct {
	ID          int64
	PublishedAt int64
}

// APIItemIDs returns a page of article ids of a stream, newest first (or
// oldest first), and the continuation for the next page ("" at the end).
func (s *Store) APIItemIDs(st APIStream, oldest bool, cont string, limit int) ([]APIItemRef, string, error) {
	where, args := st.where()
	if cont != "" {
		p, i, ok := strings.Cut(cont, "-")
		pub, err1 := strconv.ParseInt(p, 10, 64)
		id, err2 := strconv.ParseInt(i, 10, 64)
		if !ok || err1 != nil || err2 != nil {
			return nil, "", ErrBadCursor
		}
		if oldest {
			where += " AND (a.published_at > ? OR (a.published_at = ? AND a.id > ?))"
		} else {
			where += " AND (a.published_at < ? OR (a.published_at = ? AND a.id < ?))"
		}
		args = append(args, pub, pub, id)
	}
	order := "a.published_at DESC, a.id DESC"
	if oldest {
		order = "a.published_at ASC, a.id ASC"
	}
	rows, err := s.DB.Query(fmt.Sprintf(`SELECT a.id, a.published_at FROM articles a WHERE %s ORDER BY %s LIMIT %d`,
		where, order, limit+1), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	list := []APIItemRef{}
	for rows.Next() {
		var r APIItemRef
		if err := rows.Scan(&r.ID, &r.PublishedAt); err != nil {
			return nil, "", err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > limit {
		list = list[:limit]
		last := list[len(list)-1]
		next = fmt.Sprintf("%d-%d", last.PublishedAt, last.ID)
	}
	return list, next, nil
}

// APIItem is an article with what the API returns about it.
type APIItem struct {
	ID          int64
	FeedID      int64
	FeedTitle   string
	SiteURL     string
	Folder      string
	Title       string
	URL         string
	Author      string
	Content     string // the full text when there is one
	PublishedAt int64
	FetchedAt   int64
	Read        bool
	Starred     bool
}

// APIItems returns the articles of the user with these ids, in that order
// (unknown ids are skipped).
func (s *Store) APIItems(userID int64, ids []int64) ([]*APIItem, error) {
	byID := map[int64]*APIItem{}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		args := []any{userID}
		marks := make([]string, len(chunk))
		for i, id := range chunk {
			marks[i] = "?"
			args = append(args, id)
		}
		rows, err := s.DB.Query(`SELECT a.id, a.feed_id, COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url), f.site_url,
			COALESCE(fo.name, ''), a.title, a.url, a.author, CASE WHEN a.full_content <> '' THEN a.full_content ELSE a.content END,
			a.published_at, a.fetched_at, a.read, a.starred
			FROM articles a JOIN feeds f ON f.id = a.feed_id LEFT JOIN folders fo ON fo.id = f.folder_id
			WHERE a.user_id = ? AND a.id IN (`+strings.Join(marks, ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			it := &APIItem{}
			if err := rows.Scan(&it.ID, &it.FeedID, &it.FeedTitle, &it.SiteURL, &it.Folder, &it.Title, &it.URL, &it.Author,
				&it.Content, &it.PublishedAt, &it.FetchedAt, &it.Read, &it.Starred); err != nil {
				rows.Close()
				return nil, err
			}
			byID[it.ID] = it
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	list := make([]*APIItem, 0, len(byID))
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			list = append(list, it)
			delete(byID, id) // an id asked for twice is returned once
		}
	}
	return list, nil
}

// SetReadIDs marks articles as read or unread; reading one also reads its
// copies from other feeds, as in the app.
func (s *Store) SetReadIDs(userID int64, ids []int64, read bool) error {
	if !read {
		_, err := s.MarkUnread(userID, ids)
		return err
	}
	changed, err := s.MarkReadIDs(userID, ids)
	if err != nil {
		return err
	}
	for _, id := range changed {
		if _, err := s.ReadDuplicates(userID, id); err != nil {
			return err
		}
	}
	return nil
}

// SetStarredIDs stars or unstars articles.
func (s *Store) SetStarredIDs(userID int64, ids []int64, starred bool) error {
	if starred {
		_, err := s.updateIDs(`UPDATE articles SET starred = 1, starred_at = ? WHERE user_id = ? AND starred = 0 AND id IN (%s) RETURNING id`,
			[]any{now(), userID}, ids)
		return err
	}
	_, err := s.updateIDs(`UPDATE articles SET starred = 0, starred_at = 0 WHERE user_id = ? AND starred = 1 AND id IN (%s) RETURNING id`,
		[]any{userID}, ids)
	return err
}

// APIMarkAllRead marks a stream as read, up to the articles published at
// or before until (0 = all).
func (s *Store) APIMarkAllRead(st APIStream, until int64) (int64, error) {
	st.Unread = true
	st.Until = 0
	where, args := st.where()
	if until > 0 {
		where += " AND a.published_at <= ?"
		args = append(args, until)
	}
	// Duplicates follow their first copy, as in the app.
	res, err := s.DB.Exec(`UPDATE articles SET read = 1 WHERE id IN (SELECT a.id FROM articles a WHERE `+where+`)
		OR (user_id = ? AND read = 0 AND dup_of IN (SELECT a.id FROM articles a WHERE `+where+`))`,
		append(append(append([]any{}, args...), st.UserID), args...)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
