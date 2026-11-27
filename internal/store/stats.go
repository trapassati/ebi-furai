package store

import (
	"database/sql"
)

// Feed statistics: what arrives and what is opened, for the page that
// tells which feeds are read, which are ignored and which have stopped.
// Opens come from feed_opens (counted since version 1.9.0); arrivals are
// the articles published in the period that the user can see.

type FeedStat struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	FolderID    *int64 `json:"folder_id"`
	HasIcon     bool   `json:"has_icon"`
	Arrived     int    `json:"arrived"`      // articles published in the period
	Opened      int    `json:"opened"`       // articles opened in the period
	LastOpened  int64  `json:"last_opened"`  // day (since the epoch, UTC) of the last open, 0 never
	LastArticle int64  `json:"last_article"` // unix time of the newest article, 0 none
	Since       int64  `json:"since"`        // first day with data for this feed
}

type Stats struct {
	Days  int         `json:"days"`
	Today int64       `json:"today"` // day since the epoch, UTC
	Since int64       `json:"since"` // first day with opens counted, for this user
	Daily []int       `json:"daily"` // opens per day, oldest first, today last
	Feeds []*FeedStat `json:"feeds"`
}

// FeedStats returns the statistics of the user's feeds over the last days.
func (s *Store) FeedStats(userID int64, days int) (*Stats, error) {
	today := now() / 86400
	first := today - int64(days) + 1
	out := &Stats{Days: days, Today: today, Since: today, Daily: make([]int, days), Feeds: []*FeedStat{}}

	// Opens are counted since an update: before the first one there is no
	// data, not a lack of reading.
	var since sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MIN(o.day) FROM feed_opens o JOIN feeds f ON f.id = o.feed_id WHERE f.user_id = ?`, userID).Scan(&since); err != nil {
		return nil, err
	}
	if since.Valid && since.Int64 < today {
		out.Since = since.Int64
	}

	rows, err := s.DB.Query(`SELECT f.id, f.folder_id, f.title, f.custom_title, f.url, f.icon IS NOT NULL, f.created_at,
		(SELECT COALESCE(MAX(a.published_at), 0) FROM articles a WHERE a.feed_id = f.id)
		FROM feeds f WHERE f.user_id = ? AND f.kind = 'feed'`, userID)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*FeedStat{}
	for rows.Next() {
		f := &FeedStat{}
		var folder sql.NullInt64
		var title, custom, url string
		var created int64
		if err := rows.Scan(&f.ID, &folder, &title, &custom, &url, &f.HasIcon, &created, &f.LastArticle); err != nil {
			rows.Close()
			return nil, err
		}
		if folder.Valid {
			f.FolderID = &folder.Int64
		}
		f.Title = displayTitle(custom, title, url)
		f.Since = max(out.Since, created/86400)
		byID[f.ID] = f
		out.Feeds = append(out.Feeds, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// What arrived: the articles the user sees in the lists (not hidden by
	// a filter or as a Short, not the second copy of the same news).
	rows, err = s.DB.Query(`SELECT feed_id, COUNT(*) FROM articles
		WHERE user_id = ? AND published_at >= ? AND hidden_by IS NULL AND dup_of IS NULL AND saved = 0
		GROUP BY feed_id`, userID, first*86400)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if f := byID[id]; f != nil {
			f.Arrived = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.DB.Query(`SELECT o.feed_id, o.day, o.opens FROM feed_opens o JOIN feeds f ON f.id = o.feed_id
		WHERE f.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, day int64
		var n int
		if err := rows.Scan(&id, &day, &n); err != nil {
			return nil, err
		}
		f := byID[id]
		if f == nil {
			continue
		}
		f.LastOpened = max(f.LastOpened, day)
		if day >= first && day <= today {
			f.Opened += n
			out.Daily[day-first] += n
		}
	}
	return out, rows.Err()
}
