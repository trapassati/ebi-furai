package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/trapassati/ebi-furai/internal/db"
)

// Undoing "mark all as read" brings back only what it changed.
func TestMarkAllReadUndo(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	uid, _ := res.LastInsertId()
	fid, err := st.CreateFeed(uid, "https://example.com/feed", "F", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var items []NewArticle
	for i := range 10 {
		items = append(items, NewArticle{GUIDHash: fmt.Sprint("g", i), Title: fmt.Sprint("t", i), PublishedAt: int64(100 + i)})
	}
	if _, err := st.InsertArticles(fid, uid, false, items); err != nil {
		t.Fatal(err)
	}
	// Three were already read before.
	conn.Exec(`UPDATE articles SET read = 1 WHERE id IN (SELECT id FROM articles ORDER BY id LIMIT 3)`)
	unread := func() (n int) {
		conn.QueryRow(`SELECT COUNT(*) FROM articles WHERE read = 0`).Scan(&n)
		return
	}
	ids, err := st.MarkAllRead(Selection{UserID: uid, Kind: "all"}, st.MaxArticleID(uid))
	if err != nil || len(ids) != 7 || unread() != 0 {
		t.Fatalf("marked %v (%v), unread %d", ids, err, unread())
	}
	// Another user's articles are never touched by an undo.
	if n, _ := st.MarkUnread(uid+1, ids); n != 0 {
		t.Fatalf("undo for another user changed %d articles", n)
	}
	if n, err := st.MarkUnread(uid, ids); err != nil || n != 7 || unread() != 7 {
		t.Fatalf("restored %d (%v), unread %d", n, err, unread())
	}
}
