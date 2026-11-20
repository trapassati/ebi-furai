package feeds

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/store"
)

type fakeFeed string

func (f fakeFeed) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/rss+xml"}},
		Body: io.NopCloser(strings.NewReader(string(f))), Request: r}, nil
}

const rss = `<?xml version="1.0"?><rss version="2.0"><channel><title>Notizie</title><link>https://news.example/</link>
<item><guid>1</guid><title>Il calcio di oggi</title><link>https://news.example/1</link><description>Partita.</description></item>
<item><guid>2</guid><title>L'oroscopo della settimana</title><link>https://news.example/2</link><description>Stelle.</description></item>
<item><guid>3</guid><title>Politica estera</title><link>https://news.example/3</link><description>Si parla anche di calcio.</description></item>
</channel></rss>`

// New articles go through the user's filters as they arrive, and the
// visible ones of a full-text feed wait for the download.
func TestFetchAppliesFilters(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	uid, _ := res.LastInsertId()
	fid, _ := st.CreateFeed(uid, "https://news.example/feed.xml", "", "", 0)
	hide := &store.FilterInfo{Terms: []string{"calcio"}, Action: "hide"}
	st.CreateFilter(uid, hide)
	st.CreateFilter(uid, &store.FilterInfo{Terms: []string{"oroscopo"}, Action: "read"})
	if err := st.SetFeedFullText(uid, fid, true); err != nil {
		t.Fatal(err)
	}

	f := &Fetcher{store: st, cfg: &config.Config{RetentionDays: 90, FetchWorkers: 1, RefreshInterval: config.MinRefresh}, client: &http.Client{Transport: fakeFeed(rss)},
		wake: make(chan struct{}, 1), fullWake: make(chan struct{}, 1)}
	feed, err := st.FetchFeedByID(uid, fid)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.fetchFeed(context.Background(), feed); err != nil {
		t.Fatal(err)
	}
	type row struct {
		read, tried int
		hidden      bool
	}
	got := map[string]row{}
	rows, _ := conn.Query(`SELECT title, read, full_tried, hidden_by IS NOT NULL FROM articles`)
	for rows.Next() {
		var title string
		var r row
		rows.Scan(&title, &r.read, &r.tried, &r.hidden)
		got[title] = r
	}
	rows.Close()
	want := map[string]row{
		"Il calcio di oggi":          {1, 1, true},  // hidden (title)
		"L'oroscopo della settimana": {1, 1, false}, // read, no full text needed
		"Politica estera":            {0, 0, false}, // "calcio" only in the text: title-only filter
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %+v, want %+v", k, got[k], w)
		}
	}
	select {
	case <-f.fullWake:
	default:
		t.Error("the full-text downloader was not woken")
	}
}
