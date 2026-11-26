package feeds

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/netsafe"
)

type Candidate struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

var (
	ErrNoFeed       = errors.New("no feed found at this address")
	ErrAuthRequired = errors.New("this feed needs a username and password")
	ErrAuthRejected = errors.New("username or password not accepted")
)

// Credentials for a feed behind HTTP Basic authentication.
type Credentials struct{ User, Pass string }

// NormalizeURL adds https:// when the scheme is missing.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	return raw
}

// Discover returns the feed(s) available at raw: the URL itself when it is
// a feed, otherwise the feeds advertised by the page or found at common
// paths.
func (f *Fetcher) Discover(ctx context.Context, raw string, cred *Credentials) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	raw = NormalizeURL(raw)
	if _, err := netsafe.ValidateURL(raw); err != nil {
		return nil, err
	}
	if cred == nil {
		// A channel, handle, playlist or video: the feed YouTube publishes
		// for it (its pages do not show it, and from the EU they are a
		// cookie banner).
		yt, err := f.youTubeFeed(ctx, raw)
		if err != nil {
			return nil, err
		}
		if yt != "" {
			raw = yt
		}
	}
	body, final, ctype, err := f.get(ctx, raw, cred)
	if he, ok := errors.AsType[*httpError](err); ok && he.status == http.StatusUnauthorized {
		if cred != nil {
			return nil, ErrAuthRejected
		}
		return nil, ErrAuthRequired
	}
	if err != nil {
		return nil, err
	}
	if feed, err := gofeed.NewParser().Parse(bytes.NewReader(body)); err == nil {
		return []Candidate{{URL: final.String(), Title: strings.TrimSpace(feed.Title)}}, nil
	}
	if !strings.Contains(ctype, "html") && ctype != "" {
		return nil, ErrNoFeed
	}

	cands := linkedFeeds(body, final)
	if len(cands) > 0 {
		return cands, nil
	}

	origin := &url.URL{Scheme: final.Scheme, Host: final.Host}
	for _, p := range []string{"/feed", "/rss", "/feed.xml", "/rss.xml", "/atom.xml", "/index.xml", "/feed.json"} {
		u := origin.ResolveReference(&url.URL{Path: p}).String()
		b, fu, _, err := f.get(ctx, u, cred)
		if err != nil {
			continue
		}
		if feed, err := gofeed.NewParser().Parse(bytes.NewReader(b)); err == nil {
			return []Candidate{{URL: fu.String(), Title: strings.TrimSpace(feed.Title)}}, nil
		}
	}
	return nil, ErrNoFeed
}

func (f *Fetcher) get(ctx context.Context, raw string, cred *Credentials) ([]byte, *url.URL, string, error) {
	headers := map[string]string{
		// Browser-like Accept: some CDNs challenge unusual values on HTML pages.
		"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}
	if cred != nil {
		headers["Authorization"] = BasicAuth(cred.User, cred.Pass)
	}
	resp, err := netsafe.Get(ctx, f.client, raw, headers)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", &httpError{status: resp.StatusCode}
	}
	body, err := netsafe.ReadLimited(resp.Body, maxFeedSize)
	if err != nil {
		return nil, nil, "", err
	}
	return body, resp.Request.URL, strings.ToLower(resp.Header.Get("Content-Type")), nil
}

var feedTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/feed+json": true,
	"application/json":      true,
	"application/rdf+xml":   true,
	"text/xml":              true,
	"application/xml":       true,
}

// linkedFeeds extracts <link rel="alternate" type="..."> feeds from HTML.
func linkedFeeds(body []byte, base *url.URL) []Candidate {
	z := html.NewTokenizer(bytes.NewReader(body))
	seen := map[string]bool{}
	var out []Candidate
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) == "body" {
			return out
		}
		if string(name) != "link" || !hasAttr {
			continue
		}
		attrs := map[string]string{}
		for {
			k, v, more := z.TagAttr()
			attrs[strings.ToLower(string(k))] = string(v)
			if !more {
				break
			}
		}
		rel := strings.ToLower(attrs["rel"])
		typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
		if !strings.Contains(rel, "alternate") || !feedTypes[typ] || attrs["href"] == "" {
			continue
		}
		u := content.ResolveURL(base, attrs["href"])
		if !content.IsHTTPURL(u) || seen[u] {
			continue
		}
		seen[u] = true
		title := strings.TrimSpace(attrs["title"])
		if title == "" {
			title = u
		}
		out = append(out, Candidate{URL: u, Title: title})
		if len(out) >= 10 {
			return out
		}
	}
}
