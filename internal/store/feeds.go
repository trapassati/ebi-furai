package store

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
)

var (
	ErrFeedExists   = errors.New("you are already subscribed to this feed")
	ErrFolderExists = errors.New("a folder with this name already exists")
)

type FeedInfo struct {
	ID            int64  `json:"id"`
	FolderID      *int64 `json:"folder_id"`
	Title         string `json:"title"`
	CustomTitle   string `json:"custom_title"`
	OriginalTitle string `json:"original_title"`
	URL           string `json:"url"`
	SiteURL       string `json:"site_url"`
	Unread        int    `json:"unread"`
	HasIcon       bool   `json:"has_icon"`
	ErrorCount    int    `json:"error_count"`
	LastError     string `json:"last_error"`
	LastFetchedAt int64  `json:"last_fetched_at"`
	LastOKAt      int64  `json:"last_ok_at"`          // last successful fetch, 0 = never
	AuthUser      string `json:"auth_user,omitempty"` // the password is never returned
	FullText      bool   `json:"full_text"`           // download the full text of new articles
	OpenWeb       bool   `json:"open_web"`            // open articles on the site
	Instant       bool   `json:"instant"`             // new articles arrive through WebSub
}

type FolderInfo struct {
	ID       int64       `json:"id"`
	Name     string      `json:"name"`
	Position int         `json:"position"`
	Unread   int         `json:"unread"`
	Feeds    []*FeedInfo `json:"feeds"`
}

type Sources struct {
	Today      int           `json:"today"`
	TodayTotal int           `json:"today_total"`
	Unread     int           `json:"unread"`
	Starred    int           `json:"starred"`
	Saved      int           `json:"saved"`       // pages in "Read later" still to read
	SavedTotal int           `json:"saved_total"` // all the pages in "Read later"
	Folders    []*FolderInfo `json:"folders"`
	Feeds      []*FeedInfo   `json:"feeds"`
}

// Sources returns the sidebar tree with unread counters for a user.
func (s *Store) Sources(userID, todayStart int64) (*Sources, error) {
	out := &Sources{Folders: []*FolderInfo{}, Feeds: []*FeedInfo{}}
	err := s.DB.QueryRow(`SELECT
		(SELECT COUNT(*) FROM articles WHERE user_id = ? AND read = 0 AND published_at >= ? AND saved = 0),
		(SELECT COUNT(*) FROM articles WHERE user_id = ? AND published_at >= ? AND hidden_by IS NULL AND dup_of IS NULL AND saved = 0),
		(SELECT COUNT(*) FROM articles WHERE user_id = ? AND read = 0 AND saved = 0),
		(SELECT COUNT(*) FROM articles WHERE user_id = ? AND starred = 1),
		(SELECT COUNT(*) FROM articles WHERE user_id = ? AND saved = 1 AND read = 0),
		(SELECT COUNT(*) FROM articles WHERE user_id = ? AND saved = 1)`,
		userID, todayStart, userID, todayStart, userID, userID, userID, userID).Scan(&out.Today, &out.TodayTotal, &out.Unread, &out.Starred,
		&out.Saved, &out.SavedTotal)
	if err != nil {
		return nil, err
	}

	unread := map[int64]int{}
	rows, err := s.DB.Query(`SELECT feed_id, COUNT(*) FROM articles WHERE user_id = ? AND read = 0 GROUP BY feed_id`, userID)
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
		unread[id] = n
	}
	rows.Close()

	folders := map[int64]*FolderInfo{}
	rows, err = s.DB.Query(`SELECT id, name, position FROM folders WHERE user_id = ? ORDER BY position, name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		f := &FolderInfo{Feeds: []*FeedInfo{}}
		if err := rows.Scan(&f.ID, &f.Name, &f.Position); err != nil {
			rows.Close()
			return nil, err
		}
		folders[f.ID] = f
		out.Folders = append(out.Folders, f)
	}
	rows.Close()

	rows, err = s.DB.Query(`SELECT id, folder_id, title, custom_title, url, site_url, icon IS NOT NULL,
		error_count, last_error, last_fetched_at, last_ok_at, auth_user, full_text, open_web,
		`+instantCol+` FROM feeds WHERE user_id = ? AND kind = 'feed'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []*FeedInfo
	for rows.Next() {
		f := &FeedInfo{}
		var folder sql.NullInt64
		if err := rows.Scan(&f.ID, &folder, &f.OriginalTitle, &f.CustomTitle, &f.URL, &f.SiteURL, &f.HasIcon,
			&f.ErrorCount, &f.LastError, &f.LastFetchedAt, &f.LastOKAt, &f.AuthUser, &f.FullText, &f.OpenWeb, &f.Instant); err != nil {
			return nil, err
		}
		if folder.Valid {
			f.FolderID = &folder.Int64
		}
		f.Title = displayTitle(f.CustomTitle, f.OriginalTitle, f.URL)
		f.Unread = unread[f.ID]
		all = append(all, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortFeeds(all)
	for _, f := range all {
		if f.FolderID != nil {
			if folder, ok := folders[*f.FolderID]; ok {
				folder.Feeds = append(folder.Feeds, f)
				folder.Unread += f.Unread
				continue
			}
		}
		out.Feeds = append(out.Feeds, f)
	}
	return out, nil
}

func displayTitle(custom, title, url string) string {
	if strings.TrimSpace(custom) != "" {
		return custom
	}
	if strings.TrimSpace(title) != "" {
		return title
	}
	return url
}

func sortFeeds(list []*FeedInfo) {
	slices.SortStableFunc(list, func(a, b *FeedInfo) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
}

// ---- folders ----

func (s *Store) CreateFolder(userID int64, name string) (int64, error) {
	var pos int
	s.DB.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM folders WHERE user_id = ?`, userID).Scan(&pos)
	res, err := s.DB.Exec(`INSERT INTO folders(user_id, name, position) VALUES(?, ?, ?)`, userID, name, pos)
	if isUnique(err) {
		return 0, ErrFolderExists
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FolderByName returns the id of a folder, creating it if missing.
func (s *Store) FolderByName(userID int64, name string) (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM folders WHERE user_id = ? AND name = ?`, userID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return s.CreateFolder(userID, name)
}

func (s *Store) RenameFolder(userID, id int64, name string) error {
	err := execOne(s.DB, `UPDATE folders SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)
	if isUnique(err) {
		return ErrFolderExists
	}
	return err
}

func (s *Store) SetFolderPosition(userID, id int64, pos int) error {
	return execOne(s.DB, `UPDATE folders SET position = ? WHERE id = ? AND user_id = ?`, pos, id, userID)
}

// DeleteFolder removes a folder; its feeds move to the root.
func (s *Store) DeleteFolder(userID, id int64) error {
	return execOne(s.DB, `DELETE FROM folders WHERE id = ? AND user_id = ?`, id, userID)
}

func (s *Store) FolderExists(userID, id int64) bool {
	var x int
	return s.DB.QueryRow(`SELECT 1 FROM folders WHERE id = ? AND user_id = ?`, id, userID).Scan(&x) == nil
}

func (s *Store) FolderNames(userID int64) (map[int64]string, error) {
	rows, err := s.DB.Query(`SELECT id, name FROM folders WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		m[id] = n
	}
	return m, rows.Err()
}

// ---- feeds ----

// CreateFeed subscribes userID to url. folderID 0 means root.
func (s *Store) CreateFeed(userID int64, url, title, siteURL string, folderID int64) (int64, error) {
	var folder any
	if folderID > 0 {
		folder = folderID
	}
	res, err := s.DB.Exec(`INSERT INTO feeds(user_id, folder_id, url, title, site_url, created_at, next_fetch_at)
		VALUES(?, ?, ?, ?, ?, ?, 0)`, userID, folder, url, title, siteURL, now())
	if isUnique(err) {
		return 0, ErrFeedExists
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FeedIDByURL(userID int64, url string) (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM feeds WHERE user_id = ? AND url = ?`, userID, url).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) FeedInfo(userID, id int64) (*FeedInfo, error) {
	src, err := s.Sources(userID, 0)
	if err != nil {
		return nil, err
	}
	for _, f := range src.Feeds {
		if f.ID == id {
			return f, nil
		}
	}
	for _, folder := range src.Folders {
		for _, f := range folder.Feeds {
			if f.ID == id {
				return f, nil
			}
		}
	}
	return nil, ErrNotFound
}

func (s *Store) RenameFeed(userID, id int64, title string) error {
	return execOne(s.DB, `UPDATE feeds SET custom_title = ? WHERE id = ? AND user_id = ?`, title, id, userID)
}

// SetFeedFullText turns the background full-text download on or off. When
// turned on, the unread articles without it are queued too.
func (s *Store) SetFeedFullText(userID, id int64, on bool) error {
	if err := execOne(s.DB, `UPDATE feeds SET full_text = ? WHERE id = ? AND user_id = ?`, on, id, userID); err != nil {
		return err
	}
	if !on {
		_, err := s.DB.Exec(`UPDATE articles SET full_tried = 1 WHERE feed_id = ? AND full_tried = 0`, id)
		return err
	}
	_, err := s.DB.Exec(`UPDATE articles SET full_tried = 0
		WHERE feed_id = ? AND user_id = ? AND read = 0 AND full_content = '' AND url <> ''`, id, userID)
	return err
}

func (s *Store) SetFeedOpenWeb(userID, id int64, on bool) error {
	return execOne(s.DB, `UPDATE feeds SET open_web = ? WHERE id = ? AND user_id = ?`, on, id, userID)
}

// MoveFeed puts a feed in a folder (folderID 0 = root).
func (s *Store) MoveFeed(userID, id, folderID int64) error {
	var folder any
	if folderID > 0 {
		if !s.FolderExists(userID, folderID) {
			return ErrNotFound
		}
		folder = folderID
	}
	return execOne(s.DB, `UPDATE feeds SET folder_id = ? WHERE id = ? AND user_id = ?`, folder, id, userID)
}

// SetFeedURL moves a feed to a new address (it moved, or the old one is
// dead): validators and errors are reset and it is fetched again soon.
func (s *Store) SetFeedURL(userID, id int64, url string) error {
	err := execOne(s.DB, `UPDATE feeds SET url = ?, etag = '', last_modified = '', error_count = 0, last_error = '',
		next_fetch_at = 0 WHERE id = ? AND user_id = ?`, url, id, userID)
	if isUnique(err) {
		return ErrFeedExists
	}
	return err
}

func (s *Store) DeleteFeed(userID, id int64) error {
	if err := execOne(s.DB, `DELETE FROM feeds WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	return s.releaseOrphanDuplicates()
}

func (s *Store) FeedIcon(userID, id int64) ([]byte, string, error) {
	var icon []byte
	var typ string
	err := s.DB.QueryRow(`SELECT icon, icon_type FROM feeds WHERE id = ? AND user_id = ? AND icon IS NOT NULL`, id, userID).Scan(&icon, &typ)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return icon, typ, err
}

// ExportFeed is used for OPML export.
type ExportFeed struct {
	Title   string
	URL     string
	SiteURL string
	Folder  string
}

func (s *Store) ExportFeeds(userID int64) ([]ExportFeed, error) {
	rows, err := s.DB.Query(`SELECT f.custom_title, f.title, f.url, f.site_url, COALESCE(d.name, '')
		FROM feeds f LEFT JOIN folders d ON d.id = f.folder_id
		WHERE f.user_id = ? AND f.kind = 'feed' ORDER BY COALESCE(d.position, 0), d.name, f.title COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ExportFeed
	for rows.Next() {
		var custom, title string
		var e ExportFeed
		if err := rows.Scan(&custom, &title, &e.URL, &e.SiteURL, &e.Folder); err != nil {
			return nil, err
		}
		e.Title = displayTitle(custom, title, e.URL)
		list = append(list, e)
	}
	return list, rows.Err()
}
