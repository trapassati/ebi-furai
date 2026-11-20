package store

import (
	"fmt"
	"strings"
)

// MaxOfflineStarred caps the starred articles included in the offline set.
const MaxOfflineStarred = 500

// OfflineState is the read/starred state of an article of the offline set.
type OfflineState struct {
	ID      int64
	Read    bool
	Starred bool
}

// OfflineSet returns the articles a device should keep offline: the newest
// `limit` unread articles plus the starred ones, newest first.
func (s *Store) OfflineSet(userID int64, limit int) ([]OfflineState, error) {
	rows, err := s.DB.Query(`SELECT id, read, starred FROM articles
		WHERE user_id = ? AND (
			id IN (SELECT id FROM articles WHERE user_id = ? AND read = 0 ORDER BY published_at DESC, id DESC LIMIT ?)
			OR id IN (SELECT id FROM articles WHERE user_id = ? AND starred = 1 ORDER BY starred_at DESC LIMIT ?))
		ORDER BY published_at DESC, id DESC`, userID, userID, limit, userID, MaxOfflineStarred)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []OfflineState
	for rows.Next() {
		var st OfflineState
		if err := rows.Scan(&st.ID, &st.Read, &st.Starred); err != nil {
			return nil, err
		}
		list = append(list, st)
	}
	return list, rows.Err()
}

// GetArticles returns full articles by id (only those owned by the user).
func (s *Store) GetArticles(userID int64, ids []int64) ([]*Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	q := fmt.Sprintf(`SELECT %s, a.author, a.content, a.full_content, f.site_url
		FROM articles a JOIN feeds f ON f.id = a.feed_id
		WHERE a.user_id = ? AND a.id IN (%s)
		ORDER BY a.published_at DESC, a.id DESC`, summaryCols, ph)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Article
	for rows.Next() {
		a := &Article{}
		if err := rows.Scan(&a.ID, &a.FeedID, &a.FeedTitle, &a.FeedHasIcon, &a.Title, &a.URL, &a.Preview, &a.Thumbnail,
			&a.PublishedAt, &a.Read, &a.Starred, &a.Words, &a.Author, &a.Content, &a.FullContent, &a.SiteURL); err != nil {
			return nil, err
		}
		a.HasFull = a.FullContent != ""
		list = append(list, a)
	}
	return list, rows.Err()
}
