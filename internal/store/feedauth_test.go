package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/trapassati/ebi-furai/internal/db"
)

func TestFeedAuthSealed(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(conn)
	res, err := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	fid, err := st.CreateFeed(uid, "https://example.com/feed", "F", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetFeedAuth(uid, fid, "mario", "s3gr€to"); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	conn.QueryRow(`SELECT auth_secret FROM feeds WHERE id = ?`, fid).Scan(&raw)
	if len(raw) == 0 || string(raw) == "s3gr€to" {
		t.Fatalf("password not sealed: %q", raw)
	}
	ff, err := st.FetchFeedByID(uid, fid)
	if err != nil || ff.AuthUser != "mario" || ff.AuthPass != "s3gr€to" {
		t.Fatalf("got %+v, %v", ff, err)
	}
	// moved to another feed it must not open (bound to the feed id)
	if _, err := st.openPassword(fid+1, raw); err == nil {
		t.Fatal("secret opened for another feed")
	}
	src, _ := st.Sources(uid, 0)
	if len(src.Feeds) != 1 || src.Feeds[0].AuthUser != "mario" {
		t.Fatalf("auth_user not listed: %+v", src.Feeds)
	}
	if err := st.SetFeedAuth(uid, fid, "", ""); err != nil {
		t.Fatal(err)
	}
	if ff, _ = st.FetchFeedByID(uid, fid); ff.AuthUser != "" || ff.AuthPass != "" {
		t.Fatalf("not removed: %+v", ff)
	}
}

func TestSetRefreshMinutes(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	uid, _ := res.LastInsertId()
	fid, _ := st.CreateFeed(uid, "https://example.com/feed", "F", "", 0)
	conn.Exec(`UPDATE feeds SET last_fetched_at = 1000, next_fetch_at = 1000 + 3600 WHERE id = ?`, fid)
	if err := st.SetRefreshMinutes(uid, 10, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	var next int64
	conn.QueryRow(`SELECT next_fetch_at FROM feeds WHERE id = ?`, fid).Scan(&next)
	if next != 1000+600 {
		t.Fatalf("next_fetch_at = %d, want %d", next, 1600)
	}
	ff, _ := st.FetchFeedByID(uid, fid)
	if ff.RefreshMinutes != 10 {
		t.Fatalf("RefreshMinutes = %d", ff.RefreshMinutes)
	}
	due, _ := st.DueFeeds(10)
	if len(due) != 1 || due[0].RefreshMinutes != 10 {
		t.Fatalf("due feeds: %+v", due)
	}
}
