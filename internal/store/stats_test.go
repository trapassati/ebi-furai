package store

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/trapassati/ebi-furai/internal/db"
)

func statsStore(t *testing.T) (*Store, int64) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at, lang) VALUES('u', 'x', 0, 0, 'it')`)
	uid, _ := res.LastInsertId()
	return st, uid
}

// The statistics count what arrived in the period and what was opened,
// per feed and per day, and only for the user's own feeds.
func TestFeedStats(t *testing.T) {
	st, uid := statsStore(t)
	other, _ := st.DB.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at, lang) VALUES('o', 'x', 0, 0, 'it')`)
	oid, _ := other.LastInsertId()
	read, _ := st.CreateFeed(uid, "https://example.com/read", "Read", "", 0)
	quiet, _ := st.CreateFeed(uid, "https://example.com/quiet", "Quiet", "", 0)
	foreign, _ := st.CreateFeed(oid, "https://example.com/other", "Other", "", 0)
	today := now() / 86400
	var items []NewArticle
	for i := range 6 {
		items = append(items, NewArticle{GUIDHash: fmt.Sprint("r", i), Title: "A", PublishedAt: now() - int64(i)*3600})
	}
	// One old article, outside the 30 days.
	items = append(items, NewArticle{GUIDHash: "old", Title: "Old", PublishedAt: now() - 40*86400})
	if _, err := st.InsertArticles(read, uid, false, items); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertArticles(quiet, uid, false, []NewArticle{{GUIDHash: "q", Title: "Q", PublishedAt: now() - 100*86400}}); err != nil {
		t.Fatal(err)
	}
	// A hidden article does not count as arrived.
	st.DB.Exec(`UPDATE articles SET hidden_by = -1 WHERE guid_hash = 'r5'`)
	st.DB.Exec(`INSERT INTO feed_opens(feed_id, day, opens) VALUES(?, ?, 2), (?, ?, 1), (?, ?, 9), (?, ?, 4)`,
		read, today, read, today-3, read, today-60, foreign, today)

	s, err := st.FeedStats(uid, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Daily) != 30 || s.Daily[29] != 2 || s.Daily[26] != 1 || s.Today != today {
		t.Fatalf("daily %v", s.Daily)
	}
	if s.Since != today-60 {
		t.Fatalf("since %d, want %d", s.Since, today-60)
	}
	if len(s.Feeds) != 2 {
		t.Fatalf("%d feeds", len(s.Feeds))
	}
	byID := map[int64]*FeedStat{}
	for _, f := range s.Feeds {
		byID[f.ID] = f
	}
	r, q := byID[read], byID[quiet]
	if r.Arrived != 5 || r.Opened != 3 || r.LastOpened != today {
		t.Fatalf("read feed: %+v", r)
	}
	if q.Arrived != 0 || q.Opened != 0 || q.LastOpened != 0 || q.LastArticle >= now()-99*86400 {
		t.Fatalf("quiet feed: %+v", q)
	}
	// The feed was added today: its data starts today, not before.
	if r.Since != today {
		t.Fatalf("feed since %d", r.Since)
	}
	if s90, err := st.FeedStats(uid, 90); err != nil || s90.Feeds[0].Opened+s90.Feeds[1].Opened != 12 {
		t.Fatalf("90 days: %v", err)
	}
}

// The folders take the order given; unknown ids are ignored and the
// folders left out keep their place after the others.
func TestSetFolderOrder(t *testing.T) {
	st, uid := statsStore(t)
	a, _ := st.CreateFolder(uid, "A")
	b, _ := st.CreateFolder(uid, "B")
	c, _ := st.CreateFolder(uid, "C")
	other, _ := st.DB.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at, lang) VALUES('o', 'x', 0, 0, 'it')`)
	oid, _ := other.LastInsertId()
	x, _ := st.CreateFolder(oid, "X")
	order := func() []int64 {
		src, err := st.Sources(uid, 0)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, f := range src.Folders {
			ids = append(ids, f.ID)
		}
		return ids
	}
	if err := st.SetFolderOrder(uid, []int64{c, a, x, c}); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []int64{c, a, b}) {
		t.Fatalf("order %v", got)
	}
	var pos int
	st.DB.QueryRow(`SELECT position FROM folders WHERE id = ?`, x).Scan(&pos)
	if pos != 1 {
		t.Fatalf("someone else's folder moved to %d", pos)
	}
}
