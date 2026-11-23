package content

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FetchPage reads the title, author, picture and text of a page.
func TestFetchPage(t *testing.T) {
	body := `<!doctype html><html><head><title>A long walk | The Site</title>
<meta property="og:title" content="A long walk">
<meta property="og:image" content="/pics/walk.jpg">
<meta name="author" content="Ada Rossi">
<meta property="og:site_name" content="The Site"></head>
<body><nav>Home · About</nav><article><h1>A long walk</h1>` +
		strings.Repeat(`<p>We walked along the river for hours, talking about everything and nothing, until the light went down behind the hills.</p>`, 6) +
		`</article><footer>© The Site</footer></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(body))
	}))
	defer srv.Close()
	// A made-up host name, sent to the test server: the address checks
	// refuse loopback literals.
	addr := srv.Listener.Addr().String()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	p, err := FetchPage(context.Background(), client, "http://example.test/walk")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "A long walk" || p.Byline != "Ada Rossi" || p.SiteName != "The Site" {
		t.Fatalf("title %q, byline %q, site %q", p.Title, p.Byline, p.SiteName)
	}
	if p.Image != "http://example.test/pics/walk.jpg" {
		t.Fatalf("image %q", p.Image)
	}
	if !strings.Contains(p.HTML, "walked along the river") || strings.Contains(p.HTML, "Home · About") {
		t.Fatalf("html %q", p.HTML)
	}
	full, err := FetchFullText(context.Background(), client, "http://example.test/walk")
	if err != nil || full != p.HTML {
		t.Fatalf("full text: %v", err)
	}
}
