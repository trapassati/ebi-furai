package store

import (
	"path/filepath"
	"testing"

	"github.com/trapassati/ebi-furai/internal/db"
)

// Opens are counted per feed and day, and go away with the feed.
func TestArticleOpened(t *testing.T) {
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
	var aid int64
	conn.QueryRow(`SELECT id FROM articles WHERE feed_id = ?`, fid).Scan(&aid)
	// The first open counts, opening it again does not.
	if first, err := st.OpenArticle(uid, aid); err != nil || !first {
		t.Fatalf("first open: %v %v", first, err)
	}
	if again, err := st.OpenArticle(uid, aid); err != nil || again {
		t.Fatalf("second open: %v %v", again, err)
	}
	if _, err := st.OpenArticle(uid+1, aid); err != ErrNotFound {
		t.Fatalf("someone else's article: %v", err)
	}
	for range 2 {
		if err := st.ArticleOpened(fid); err != nil {
			t.Fatal(err)
		}
	}
	var day, opens int64
	if err := conn.QueryRow(`SELECT day, opens FROM feed_opens WHERE feed_id = ?`, fid).Scan(&day, &opens); err != nil {
		t.Fatal(err)
	}
	if opens != 3 || day != now()/86400 {
		t.Fatalf("day %d, opens %d", day, opens)
	}
	if err := st.DeleteFeed(uid, fid); err != nil {
		t.Fatal(err)
	}
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM feed_opens`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows left after deleting the feed", n)
	}
}
