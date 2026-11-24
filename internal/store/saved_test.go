package store

import (
	"path/filepath"
	"testing"

	"github.com/trapassati/ebi-furai/internal/db"
)

// Pages saved for later: their own collection, kept out of the feeds, of
// Unread and of the badge, never fetched, kept until read.
func TestSavedPages(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at, lang) VALUES('u', 'x', 0, 0, 'it')`)
	uid, _ := res.LastInsertId()
	fid, err := st.CreateFeed(uid, "https://example.com/feed", "F", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertArticles(fid, uid, false, []NewArticle{{GUIDHash: "g1", Title: "News", URL: "https://example.com/a", PublishedAt: now()}}); err != nil {
		t.Fatal(err)
	}

	id, again, err := st.SavePage(uid, SavedPage{URL: "https://example.com/a", Title: "Page", Content: "<p>Hello</p>", Text: "Hello", Words: 1})
	if err != nil || again || id == 0 {
		t.Fatalf("save: %d %v %v", id, again, err)
	}
	var title, url string
	conn.QueryRow(`SELECT title, url FROM feeds WHERE user_id = ? AND kind = 'saved'`, uid).Scan(&title, &url)
	if title != "Da leggere" || url != savedFeedURL {
		t.Fatalf("saved feed %q %q", title, url)
	}

	var savedID int64
	conn.QueryRow(`SELECT id FROM feeds WHERE user_id = ? AND kind = 'saved'`, uid).Scan(&savedID)
	if err := st.DeleteFeed(uid, savedID); err != ErrNotFound {
		t.Fatalf("deleting Read later: %v", err)
	}
	if l, _, _ := st.ListArticles(Selection{UserID: uid, Kind: "saved"}, ListOptions{Limit: 50}); len(l) != 1 {
		t.Fatal("the saved page is gone")
	}

	src, err := st.Sources(uid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if src.Saved != 1 || src.SavedTotal != 1 || src.Unread != 1 || len(src.Feeds) != 1 {
		t.Fatalf("sources: saved %d/%d, unread %d, feeds %d", src.Saved, src.SavedTotal, src.Unread, len(src.Feeds))
	}
	if n, _ := st.UnreadCount(uid); n != 1 {
		t.Fatalf("badge counts %d, want 1 (the saved page is not in it)", n)
	}
	list := func(kind string) []*ArticleSummary {
		l, _, err := st.ListArticles(Selection{UserID: uid, Kind: kind}, ListOptions{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	if l := list("saved"); len(l) != 1 || !l[0].Saved || l[0].Title != "Page" {
		t.Fatalf("saved list %+v", l)
	}
	if l := list("all"); len(l) != 1 || l[0].Saved {
		t.Fatalf("all list has %d (the saved page must not be there)", len(l))
	}
	if l := list("unread"); len(l) != 1 {
		t.Fatalf("unread list has %d", len(l))
	}
	// The same address saved again: the same article, unread at the top.
	conn.Exec(`UPDATE articles SET read = 1 WHERE id = ?`, id)
	id2, again, err := st.SavePage(uid, SavedPage{URL: "https://example.com/a"})
	if err != nil || !again || id2 != id {
		t.Fatalf("saved again: %d %v %v", id2, again, err)
	}
	var read bool
	var content string
	conn.QueryRow(`SELECT read, content FROM articles WHERE id = ?`, id).Scan(&read, &content)
	if read || content != "<p>Hello</p>" {
		t.Fatalf("after saving again: read %v, content %q", read, content)
	}
	// Never fetched, never exported.
	if due, _ := st.DueFeeds(10); len(due) != 1 || due[0].ID != fid {
		t.Fatalf("due feeds %v", due)
	}
	if ex, _ := st.ExportFeeds(uid); len(ex) != 1 {
		t.Fatalf("exported %d feeds", len(ex))
	}
	// Kept by the cleanup while unread; removed when read and old.
	conn.Exec(`UPDATE articles SET published_at = 1, fetched_at = 1`)
	st.Cleanup(100)
	if l := list("saved"); len(l) != 1 {
		t.Fatal("an unread saved page was cleaned up")
	}
	conn.Exec(`UPDATE articles SET read = 1 WHERE id = ?`, id)
	st.Cleanup(100)
	if l := list("saved"); len(l) != 0 {
		t.Fatal("a read old saved page stayed")
	}
	// Deleting: only saved pages, only one's own.
	id, _, _ = st.SavePage(uid, SavedPage{URL: "https://example.com/b", Title: "B"})
	if err := st.DeleteSaved(uid+1, id); err == nil {
		t.Fatal("another user deleted the page")
	}
	if err := st.DeleteSaved(uid, id); err != nil {
		t.Fatal(err)
	}
}
