package websub

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

func TestDiscover(t *testing.T) {
	base, _ := url.Parse("https://site.example/feed.xml")
	cases := []struct {
		name      string
		header    http.Header
		body      string
		hub, self string
	}{
		{
			name: "atom",
			body: `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom">
				<title>T</title><link rel="alternate" href="https://site.example/"/>
				<link rel="hub" href="https://hub.example/"/><link rel="self" href="https://site.example/feed.xml"/>
				<entry><link rel="hub" href="https://other.example/"/></entry></feed>`,
			hub: "https://hub.example/", self: "https://site.example/feed.xml",
		},
		{
			name: "rss with atom:link, relative self, ISO-8859-1",
			body: "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss version=\"2.0\" xmlns:atom=\"http://www.w3.org/2005/Atom\"><channel>" +
				"<title>Citt\xe0</title><link>https://site.example/</link>" +
				"<atom:link rel=\"self\" href=\"/feed.xml?x=1\" type=\"application/rss+xml\"/>" +
				"<atom:link rel=\"hub\" href=\"https://pubsubhubbub.appspot.com\"/><item><title>a</title></item></channel></rss>",
			hub: "https://pubsubhubbub.appspot.com", self: "https://site.example/feed.xml?x=1",
		},
		{
			name: "hub only after the first item: not the feed's",
			body: `<rss><channel><title>T</title><item><atom:link rel="hub" href="https://hub.example/"/></item></channel></rss>`,
		},
		{
			name:   "link headers first",
			header: http.Header{"Link": {`<https://hub.example/a>; rel="hub", <https://site.example/self>; rel=self`}},
			body:   `<feed><link rel="hub" href="https://other.example/"/></feed>`,
			hub:    "https://hub.example/a", self: "https://site.example/self",
		},
		{
			name:   "link header with several rels and the self in the feed",
			header: http.Header{"Link": {`<https://hub.example/>;rel="hub other"`}},
			body:   `<feed><link rel="self" href="https://site.example/atom"/></feed>`,
			hub:    "https://hub.example/", self: "https://site.example/atom",
		},
		{
			name: "json feed",
			body: `{"version":"https://jsonfeed.org/version/1.1","feed_url":"https://site.example/feed.json",
				"hubs":[{"type":"rssCloud","url":"https://cloud.example/"},{"type":"WebSub","url":"https://hub.example/"}],"items":[]}`,
			hub: "https://hub.example/", self: "https://site.example/feed.json",
		},
		{
			name: "no hub",
			body: `<rss><channel><title>T</title><atom:link rel="self" href="https://site.example/feed.xml"/></channel></rss>`,
			self: "https://site.example/feed.xml",
		},
		{
			name: "not http",
			body: `<feed><link rel="hub" href="javascript:alert(1)"/></feed>`,
		},
	}
	for _, c := range cases {
		hub, self := Discover(c.header, []byte(c.body), base)
		if hub != c.hub || self != c.self {
			t.Errorf("%s: got hub %q self %q, want %q %q", c.name, hub, self, c.hub, c.self)
		}
	}
}

func TestParseLinkHeader(t *testing.T) {
	got := parseLinkHeader(`<https://a.example/x,y>; title="a, b"; rel="hub", <https://b.example/> ;rel=SELF ; type=text/xml`)
	if len(got) != 2 {
		t.Fatalf("links: %+v", got)
	}
	if got[0].target != "https://a.example/x,y" || !slices.Equal(got[0].rels, []string{"hub"}) {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].target != "https://b.example/" || !slices.Equal(got[1].rels, []string{"self"}) {
		t.Errorf("second: %+v", got[1])
	}
}

func TestValidSignature(t *testing.T) {
	body := []byte("<feed/>")
	// echo -n '<feed/>' | openssl dgst -sha256 -hmac secret
	sig := sign("sha256", "secret", body)
	if !ValidSignature(sig, "secret", body) {
		t.Fatal("valid signature refused")
	}
	for _, bad := range []string{"", "sha256", "sha256=zz", "md5=00", sign("sha256", "other", body), sign("sha1", "secret", []byte("x"))} {
		if ValidSignature(bad, "secret", body) {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, m := range []string{"sha1", "sha384", "SHA512"} {
		if !ValidSignature(sign(m, "secret", body), "secret", body) {
			t.Errorf("%s refused", m)
		}
	}
	if ValidSignature(sig, "", body) {
		t.Error("empty secret accepted")
	}
}
