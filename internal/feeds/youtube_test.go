package feeds

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"

	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/store"
)

func TestYouTubeTarget(t *testing.T) {
	const ch = "UC_x5XG1OV2P6uZZ5FSM9Ttw"
	cases := []struct{ in, feed, page string }{
		{"https://www.youtube.com/channel/" + ch, ytFeedBase + "?channel_id=" + ch, ""},
		{"https://m.youtube.com/channel/" + ch + "/videos", ytFeedBase + "?channel_id=" + ch, ""},
		{"https://www.youtube.com/feeds/videos.xml?channel_id=" + ch, ytFeedBase + "?channel_id=" + ch, ""},
		{"https://www.youtube.com/playlist?list=PLOU2XLYxmsIIM9h1Ybw2DuRw6o2fkNMeR", ytFeedBase + "?playlist_id=PLOU2XLYxmsIIM9h1Ybw2DuRw6o2fkNMeR", ""},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLOU2XLYxmsIIM9h1Ybw2DuRw6o2fkNMeR", ytFeedBase + "?playlist_id=PLOU2XLYxmsIIM9h1Ybw2DuRw6o2fkNMeR", ""},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ", "", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://www.youtube.com/@GoogleDevelopers", "", "https://www.youtube.com/@GoogleDevelopers"},
		{"https://www.youtube.com/@GoogleDevelopers/videos", "", "https://www.youtube.com/@GoogleDevelopers"},
		{"https://www.youtube.com/c/GoogleDevelopers", "", "https://www.youtube.com/c/GoogleDevelopers"},
		{"https://www.youtube.com/user/GoogleDevelopers", "", "https://www.youtube.com/user/GoogleDevelopers"},
		{"https://youtu.be/dQw4w9WgXcQ?si=abc", "", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://www.youtube.com/", "", ""},
		{"https://www.youtube.com/results?search_query=go", "", ""},
		{"https://example.com/channel/" + ch, "", ""},
		{"https://notyoutube.com/@x", "", ""},
	}
	for _, c := range cases {
		u, _ := url.Parse(c.in)
		feed, page := youTubeTarget(u)
		if feed != c.feed || page != c.page {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.in, feed, page, c.feed, c.page)
		}
	}
}

func TestChannelOfPage(t *testing.T) {
	const ch, other = "UCaaaaaaaaaaaaaaaaaaaaaa", "UCbbbbbbbbbbbbbbbbbbbbbb"
	channel := `<html><head><link rel="canonical" href="https://www.youtube.com/channel/` + ch + `">` +
		`<link rel="alternate" type="application/rss+xml" title="RSS" href="https://www.youtube.com/feeds/videos.xml?channel_id=` + ch + `">`
	if got := channelOfPage(channel); got != ch {
		t.Errorf("channel page: %q", got)
	}
	// A video page names other channels before the video's own details.
	video := `{"channelId":"` + other + `"} ... "videoDetails":{"videoId":"dQw4w9WgXcQ","title":"x","channelId":"` + ch + `"}`
	if got := channelOfPage(video); got != ch {
		t.Errorf("video page: %q", got)
	}
	if got := channelOfPage(`"externalId":"` + ch + `"`); got != ch {
		t.Errorf("externalId: %q", got)
	}
	if got := channelOfPage("<html>consent</html>"); got != "" {
		t.Errorf("nothing: %q", got)
	}
}

func TestIsYouTubeShort(t *testing.T) {
	for link, want := range map[string]bool{
		"https://www.youtube.com/shorts/cdVoZklIk8o":  true,
		"https://www.youtube.com/watch?v=dj62UQR7y7M": false,
		"https://example.com/shorts/cdVoZklIk8o":      false,
		"":                                            false,
	} {
		if got := IsYouTubeShort(link); got != want {
			t.Errorf("%q: %v", link, got)
		}
	}
}

const ytFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/" xmlns="http://www.w3.org/2005/Atom">
 <title>Canale</title>
 <link rel="alternate" href="https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa"/>
 <entry>
  <id>yt:video:dj62UQR7y7M</id>
  <yt:videoId>dj62UQR7y7M</yt:videoId>
  <title>Un video</title>
  <link rel="alternate" href="https://www.youtube.com/watch?v=dj62UQR7y7M"/>
  <published>2026-11-20T10:00:00+00:00</published>
  <media:group>
   <media:title>Un video</media:title>
   <media:content url="https://www.youtube.com/v/dj62UQR7y7M?version=3" type="application/x-shockwave-flash" width="640" height="390"/>
   <media:thumbnail url="https://i2.ytimg.com/vi/dj62UQR7y7M/hqdefault.jpg" width="480" height="360"/>
   <media:description>Prima riga &lt;b&gt;non grassetto&lt;/b&gt;
link: https://example.com/pagina.

Secondo paragrafo (https://example.com/a_(b)).</media:description>
  </media:group>
 </entry>
</feed>`

func TestYouTubeEntry(t *testing.T) {
	parsed, err := gofeed.NewParser().ParseString(ytFeed)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse("https://www.youtube.com/feeds/videos.xml?channel_id=UCaaaaaaaaaaaaaaaaaaaaaa")
	a, ok := buildArticle(parsed.Items[0], base, "https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa", 0)
	if !ok {
		t.Fatal("not built")
	}
	if a.Thumbnail != "https://i2.ytimg.com/vi/dj62UQR7y7M/hqdefault.jpg" {
		t.Errorf("thumbnail %q", a.Thumbnail)
	}
	for _, want := range []string{
		`<a href="https://www.youtube.com/watch?v=dj62UQR7y7M"`,
		`<img src="https://i2.ytimg.com/vi/dj62UQR7y7M/hqdefault.jpg"`,
		`&lt;b&gt;non grassetto&lt;/b&gt;`,
		`link: <a href="https://example.com/pagina"`,
		`</a>.</p>`,
		`(<a href="https://example.com/a_(b)"`,
	} {
		if !strings.Contains(a.Content, want) {
			t.Errorf("content lacks %q:\n%s", want, a.Content)
		}
	}
	if !strings.HasPrefix(a.Preview, "Prima riga <b>non grassetto</b> link:") {
		t.Errorf("preview %q", a.Preview)
	}
}

func TestLinkify(t *testing.T) {
	for in, want := range map[string]string{
		"see https://a.example/x, then":   `see <a href="https://a.example/x">https://a.example/x</a>, then`,
		"<tag> & no link":                 `&lt;tag&gt; &amp; no link`,
		"(https://a.example/wiki/A_(b))":  `(<a href="https://a.example/wiki/A_(b)">https://a.example/wiki/A_(b)</a>)`,
		"https://a.example/?q=1&r=2 done": `<a href="https://a.example/?q=1&amp;r=2">https://a.example/?q=1&amp;r=2</a> done`,
	} {
		if got := linkify(in); got != want {
			t.Errorf("%q:\n got %s\nwant %s", in, got, want)
		}
	}
}

// A channel that hides its Shorts gets them hidden (and read) as they
// arrive; the ones already there follow the switch, both ways.
func TestFetchHidesShorts(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	res, _ := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	uid, _ := res.LastInsertId()
	fid, _ := st.CreateFeed(uid, ytFeedBase+"?channel_id=UCaaaaaaaaaaaaaaaaaaaaaa", "", "", 0)
	if err := st.SetFeedHideShorts(uid, fid, true); err != nil {
		t.Fatal(err)
	}
	short := strings.Replace(ytFeed, "</feed>", `<entry><id>yt:video:cdVoZklIk8o</id><yt:videoId>cdVoZklIk8o</yt:videoId><title>Breve</title>
<link rel="alternate" href="https://www.youtube.com/shorts/cdVoZklIk8o"/><published>2026-11-21T10:00:00+00:00</published>
<media:group><media:description>Un minuto.</media:description></media:group></entry></feed>`, 1)
	f := &Fetcher{store: st, cfg: &config.Config{RetentionDays: 36500, FetchWorkers: 1, RefreshInterval: config.MinRefresh}, client: &http.Client{Transport: fakeFeed(short)},
		wake: make(chan struct{}, 1), fullWake: make(chan struct{}, 1)}
	feed, err := st.FetchFeedByID(uid, fid)
	if err != nil {
		t.Fatal(err)
	}
	if !feed.HideShorts {
		t.Fatal("the option is not read back")
	}
	if err := f.fetchFeed(context.Background(), feed); err != nil {
		t.Fatal(err)
	}
	state := func() map[string]string {
		got := map[string]string{}
		rows, _ := conn.Query(`SELECT title, read, COALESCE(hidden_by, 0) FROM articles`)
		defer rows.Close()
		for rows.Next() {
			var title string
			var read, hidden int
			rows.Scan(&title, &read, &hidden)
			got[title] = map[bool]string{true: "read", false: "unread"}[read == 1] + map[bool]string{true: " hidden", false: ""}[hidden == store.HiddenShort]
		}
		return got
	}
	if got := state(); got["Breve"] != "read hidden" || got["Un video"] != "unread" {
		t.Fatalf("after the fetch: %v", got)
	}
	if err := st.SetFeedHideShorts(uid, fid, false); err != nil {
		t.Fatal(err)
	}
	if got := state(); got["Breve"] != "read" {
		t.Fatalf("shown again: %v", got)
	}
	if err := st.SetFeedHideShorts(uid, fid, true); err != nil {
		t.Fatal(err)
	}
	if got := state(); got["Breve"] != "read hidden" || got["Un video"] != "unread" {
		t.Fatalf("hidden again: %v", got)
	}
}
