package backup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	xwebdav "golang.org/x/net/webdav"

	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/webdav"
)

// davServer is a WebDAV server in memory, behind a username and password.
func davServer(t *testing.T) (*httptest.Server, xwebdav.FileSystem) {
	fs := xwebdav.NewMemFS()
	fs.Mkdir(context.Background(), "/dav", 0o755)
	h := &xwebdav.Handler{Prefix: "", FileSystem: fs, LockSystem: xwebdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "ebi" || p != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, fs
}

func TestWebDAVClient(t *testing.T) {
	srv, _ := davServer(t)
	ctx := context.Background()
	if _, err := webdav.New(srv.URL+"/dav/?x=1", "", ""); err == nil {
		t.Fatal("an address with a query is accepted")
	}
	bad, _ := webdav.New(srv.URL+"/dav/ebi/", "ebi", "wrong")
	if err := bad.Check(ctx); !errors.Is(err, webdav.ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
	c, err := webdav.New(srv.URL+"/dav/ebi", "ebi", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Check(ctx); err != nil { // creates the folder
		t.Fatalf("check: %v", err)
	}
	file := filepath.Join(t.TempDir(), "x.db.gz")
	os.WriteFile(file, []byte("hello"), 0o600)
	if err := c.Upload(ctx, "ebi-furai-2026-01-01-000000.db.gz", file); err != nil {
		t.Fatal(err)
	}
	names, err := c.List(ctx)
	if err != nil || !slices.Equal(names, []string{"ebi-furai-2026-01-01-000000.db.gz"}) {
		t.Fatalf("list: %v %v", names, err)
	}
	if err := c.Delete(ctx, "ebi-furai-2026-01-01-000000.db.gz"); err != nil {
		t.Fatal(err)
	}
	if names, _ := c.List(ctx); len(names) != 0 {
		t.Fatalf("after delete: %v", names)
	}
}

// Every backup goes to the WebDAV folder too, where the same rotation is
// kept; files that are not backups are never touched.
func TestRemoteSync(t *testing.T) {
	srv, fs := davServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "reader.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(conn, filepath.Join(dir, "backups"), 2, 1, time.UTC)
	os.MkdirAll(m.Dir(), 0o700)
	// Three old daily backups here; on the server, two older ones, a
	// stray file and a broken upload.
	for _, d := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		os.WriteFile(filepath.Join(m.Dir(), "ebi-furai-"+d+"-120000.db.gz"), []byte(d), 0o600)
	}
	c, _ := webdav.New(srv.URL+"/dav/bk/", "ebi", "secret")
	c.Check(ctx)
	for _, n := range []string{"ebi-furai-2026-02-01-120000.db.gz", "ebi-furai-2026-01-01-120000.db.gz", "notes.txt", "ebi-furai-2026-03-02-120000.db.gz.part"} {
		f, _ := fs.OpenFile(ctx, "/dav/bk/"+n, os.O_CREATE|os.O_WRONLY, 0o644)
		f.Write([]byte("x"))
		f.Close()
	}
	m.SetRemote(func() (Remote, error) { return c, nil })
	if err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	names, _ := c.List(ctx)
	slices.Sort(names)
	// keep 2 daily (03-03, 03-02) + 1 weekly (03-01 is in another ISO week than 03-02? 2026-03-01 is a Sunday: week 9; 03-02 Monday: week 10)
	want := []string{"ebi-furai-2026-03-01-120000.db.gz", "ebi-furai-2026-03-02-120000.db.gz", "ebi-furai-2026-03-03-120000.db.gz", "notes.txt"}
	if !slices.Equal(names, want) {
		t.Fatalf("remote after sync:\n got %v\nwant %v", names, want)
	}
	st := m.RemoteStatus()
	if st.LastError != "" || st.LastOK.IsZero() || st.Count != 3 || st.Syncing {
		t.Fatalf("status %+v", st)
	}
	// A new backup is copied by itself.
	info, err := m.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		if names, _ := c.List(ctx); slices.Contains(names, info.Name) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if names, _ := c.List(ctx); !slices.Contains(names, info.Name) {
		t.Fatal("a new backup was not copied")
	}
	// A failure is reported and retried.
	m.SetRemote(func() (Remote, error) {
		bad, _ := webdav.New(srv.URL+"/dav/bk/", "ebi", "nope")
		return bad, nil
	})
	if err := m.Sync(ctx); err == nil || !strings.Contains(m.RemoteStatus().LastError, "refused") || !m.syncDue() {
		t.Fatalf("failure: %v %+v", err, m.RemoteStatus())
	}
	// Deleting here leaves the copy there.
	if err := m.Delete(info.Name); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(info.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := m.Delete("../reader.db"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a path outside the backups can be deleted")
	}
	if names, _ := c.List(ctx); !slices.Contains(names, info.Name) {
		t.Fatal("deleting the local backup removed the copy elsewhere")
	}
}
