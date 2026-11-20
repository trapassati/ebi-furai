package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/trapassati/ebi-furai/internal/db"
)

func newTestStore(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	uid, _ := res.LastInsertId()
	fid, err := st.CreateFeed(uid, "https://example.com/feed", "F", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return st, uid, fid
}

// A filter hides articles from every list and the counters; deleting it
// brings them back as unread.
func TestFilterHideAndRestore(t *testing.T) {
	st, uid, fid := newTestStore(t)
	f := &FilterInfo{Terms: []string{"calcio"}, Action: "hide"}
	if err := st.CreateFilter(uid, f); err != nil {
		t.Fatal(err)
	}
	items := []NewArticle{
		{GUIDHash: "a", Title: "Calcio", PublishedAt: 1, URL: "https://e.com/a", HiddenBy: f.ID, Read: true},
		{GUIDHash: "b", Title: "Altro", PublishedAt: 2, URL: "https://e.com/b"},
		{GUIDHash: "c", Title: "Ancora calcio", PublishedAt: 3, URL: "https://e.com/c"},
	}
	if _, err := st.InsertArticles(fid, uid, true, items); err != nil {
		t.Fatal(err)
	}
	// Only the visible unread ones wait for the full text.
	pending, _ := st.PendingFullTexts(0, 10)
	if len(pending) != 2 {
		t.Fatalf("pending full texts %v", pending)
	}
	cands, err := st.FilterCandidates(uid, 0, 0, false)
	if err != nil || len(cands) != 2 {
		t.Fatalf("candidates %v %v", cands, err)
	}
	hidden, err := st.HideArticles(uid, f.ID, []int64{cands[0].ID}) // "Ancora calcio", the newest
	if err != nil || len(hidden) != 1 {
		t.Fatalf("hidden %v %v", hidden, err)
	}
	list, _, _ := st.ListArticles(Selection{UserID: uid, Kind: "all"}, ListOptions{})
	if len(list) != 1 || list[0].Title != "Altro" {
		t.Fatalf("list shows hidden articles: %v", list)
	}
	src, _ := st.Sources(uid, 0)
	if src.Unread != 1 || src.TodayTotal != 1 {
		t.Fatalf("counters %d %d", src.Unread, src.TodayTotal)
	}
	fs, _ := st.Filters(uid)
	if len(fs) != 1 || fs[0].Hidden != 2 {
		t.Fatalf("filters %+v", fs)
	}
	n, err := st.DeleteFilter(uid, f.ID)
	if err != nil || n != 2 {
		t.Fatalf("restored %d %v", n, err)
	}
	src, _ = st.Sources(uid, 0)
	if src.Unread != 3 {
		t.Fatalf("unread after delete %d", src.Unread)
	}
	if _, err := st.DeleteFilter(uid, f.ID); err != ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}
}

func TestFilterScopeOwnership(t *testing.T) {
	st, uid, fid := newTestStore(t)
	res, _ := st.DB.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('other', 'x', 0, 0)`)
	other, _ := res.LastInsertId()
	if err := st.CreateFilter(other, &FilterInfo{Terms: []string{"x"}, Action: "read", FeedID: fid}); err != ErrNotFound {
		t.Fatalf("another user's feed: %v", err)
	}
	for i := range MaxFilters {
		if err := st.CreateFilter(uid, &FilterInfo{Terms: []string{fmt.Sprint("w", i)}, Action: "read"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CreateFilter(uid, &FilterInfo{Terms: []string{"x"}, Action: "read"}); err != ErrTooManyFilters {
		t.Fatalf("limit: %v", err)
	}
	// Deleting the feed removes its filters too.
	st.CreateFilter(other, &FilterInfo{Terms: []string{"x"}, Action: "read"})
	st.DeleteFeed(uid, fid)
}

func TestFeedFullTextQueue(t *testing.T) {
	st, uid, fid := newTestStore(t)
	items := []NewArticle{
		{GUIDHash: "a", Title: "A", PublishedAt: 1, URL: "https://e.com/a"},
		{GUIDHash: "b", Title: "B", PublishedAt: 2, URL: ""},
	}
	st.InsertArticles(fid, uid, false, items)
	if p, _ := st.PendingFullTexts(0, 10); len(p) != 0 {
		t.Fatal("nothing waits while the option is off")
	}
	if err := st.SetFeedFullText(uid, fid, true); err != nil {
		t.Fatal(err)
	}
	p, _ := st.PendingFullTexts(0, 10)
	if len(p) != 1 || p[0].URL != "https://e.com/a" {
		t.Fatalf("turning it on queues the unread ones with a link: %v", p)
	}
	st.FullTextDone(p[0].ID, "<p>tutto</p>", 1)
	a, _ := st.GetArticle(uid, p[0].ID)
	if !a.HasFull {
		t.Fatal("full text saved")
	}
	if p, _ := st.PendingFullTexts(0, 10); len(p) != 0 {
		t.Fatal("done articles leave the queue")
	}
	info, _ := st.FeedInfo(uid, fid)
	if !info.FullText || info.OpenWeb {
		t.Fatalf("feed info %+v", info)
	}
}
