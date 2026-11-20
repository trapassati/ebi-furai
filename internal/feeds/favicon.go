package feeds

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
)

const maxIconSize = 300 << 10

// updateIcon looks for the site's icon and stores it. Failures only record
// the check time, so it is retried after a week.
func (f *Fetcher) updateIcon(ctx context.Context, feed *store.FetchFeed) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	site := feed.SiteURL
	if site == "" {
		site = feed.URL
	}
	base, err := url.Parse(site)
	if err != nil || base.Host == "" {
		f.store.SetFeedIcon(feed.ID, nil, "")
		return
	}
	origin := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: "/"}

	var candidates []string
	if body, final, _, err := f.getLimited(ctx, origin.String(), 1<<20); err == nil {
		candidates = iconLinks(body, final)
	}
	candidates = append(candidates, origin.ResolveReference(&url.URL{Path: "/favicon.ico"}).String())

	for _, c := range candidates {
		data, typ, err := f.fetchImage(ctx, c)
		if err == nil {
			f.store.SetFeedIcon(feed.ID, data, typ)
			return
		}
	}
	f.store.SetFeedIcon(feed.ID, nil, "")
}

func (f *Fetcher) getLimited(ctx context.Context, raw string, max int64) ([]byte, *url.URL, string, error) {
	resp, err := netsafe.Get(ctx, f.client, raw, nil)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", &httpError{status: resp.StatusCode}
	}
	body, err := netsafe.ReadLimited(resp.Body, max)
	if err != nil {
		return nil, nil, "", err
	}
	return body, resp.Request.URL, resp.Header.Get("Content-Type"), nil
}

func (f *Fetcher) fetchImage(ctx context.Context, raw string) ([]byte, string, error) {
	body, _, ctype, err := f.getLimited(ctx, raw, maxIconSize)
	if err != nil {
		return nil, "", err
	}
	if len(body) < 16 {
		return nil, "", &httpError{status: 204}
	}
	typ := http.DetectContentType(body)
	if strings.HasPrefix(typ, "image/") {
		return body, typ, nil
	}
	if strings.Contains(ctype, "svg") && bytes.Contains(body[:min(len(body), 1024)], []byte("<svg")) {
		return body, "image/svg+xml", nil
	}
	return nil, "", &httpError{status: 415}
}

// iconLinks returns icon URLs declared in the page head, best first.
func iconLinks(body []byte, base *url.URL) []string {
	z := html.NewTokenizer(bytes.NewReader(body))
	var apple, icons []string
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) == "body" {
			break
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
		href := content.ResolveURL(base, attrs["href"])
		if !content.IsHTTPURL(href) {
			continue
		}
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			apple = append(apple, href)
		case strings.Contains(rel, "icon"):
			icons = append(icons, href)
		}
	}
	return append(apple, icons...)
}
