// Package feeds polls RSS/Atom/JSON feeds, discovers feeds from web pages,
// fetches favicons and handles OPML.
package feeds

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/filter"
	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
	"github.com/trapassati/ebi-furai/internal/websub"
)

const (
	maxFeedSize  = 10 << 20
	maxBackoff   = 24 * time.Hour
	iconRecheck  = 7 * 24 * time.Hour
	fetchTimeout = 30 * time.Second
	// instantPoll: a feed whose hub sends WebSub notifications is still
	// fetched now and then, in case a notification gets lost.
	instantPoll = 3 * time.Hour
)

type Fetcher struct {
	store    *store.Store
	cfg      *config.Config
	client   *http.Client
	inflight sync.Map // feed id -> struct{}
	wake     chan struct{}
	fullWake chan struct{}   // new articles wait for their full text
	websub   *websub.Manager // nil without PUBLIC_URL
}

func New(st *store.Store, cfg *config.Config) *Fetcher {
	return &Fetcher{
		store:    st,
		cfg:      cfg,
		client:   netsafe.NewClient(fetchTimeout),
		wake:     make(chan struct{}, 1),
		fullWake: make(chan struct{}, 1),
	}
}

// SetWebSub turns on instant updates: the hubs that feeds name are noted
// after each download.
func (f *Fetcher) SetWebSub(m *websub.Manager) { f.websub = m }

// Client exposes the SSRF-safe HTTP client (used for pages and images).
func (f *Fetcher) Client() *http.Client { return f.client }

// Wake asks the scheduler to look for due feeds immediately.
func (f *Fetcher) Wake() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// WakeFullText asks the full-text downloader to look for new work.
func (f *Fetcher) WakeFullText() {
	select {
	case f.fullWake <- struct{}{}:
	default:
	}
}

// Run starts the scheduler, the workers and the periodic maintenance. It
// blocks until ctx is cancelled.
func (f *Fetcher) Run(ctx context.Context) {
	jobs := make(chan *store.FetchFeed)
	var wg sync.WaitGroup
	wg.Go(func() { f.runFullText(ctx) })
	wg.Go(func() { f.countOld(ctx) })
	for range f.cfg.FetchWorkers {
		wg.Go(func() {
			for feed := range jobs {
				f.fetchTracked(ctx, feed)
			}
		})
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastMaintenance := time.Time{}
	for {
		if time.Since(lastMaintenance) > 6*time.Hour {
			f.maintenance()
			lastMaintenance = time.Now()
		}
		f.dispatch(ctx, jobs)
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case <-ticker.C:
		case <-f.wake:
		}
	}
}

func (f *Fetcher) dispatch(ctx context.Context, jobs chan<- *store.FetchFeed) {
	due, err := f.store.DueFeeds(500)
	if err != nil {
		slog.Error("due feeds", "err", err)
		return
	}
	for _, feed := range due {
		if _, busy := f.inflight.Load(feed.ID); busy {
			continue
		}
		select {
		case jobs <- feed:
		case <-ctx.Done():
			return
		}
	}
}

func (f *Fetcher) maintenance() {
	cutoff := time.Now().AddDate(0, 0, -f.cfg.RetentionDays).Unix()
	if n, err := f.store.Cleanup(cutoff); err != nil {
		slog.Error("cleanup", "err", err)
	} else if n > 0 {
		slog.Info("cleanup", "deleted", n)
	}
	if err := f.store.PurgeExpiredSessions(); err != nil {
		slog.Error("purge sessions", "err", err)
	}
	f.store.Optimize()
}

// fetchTracked fetches a feed unless another goroutine is already on it.
// It reports whether the fetch ran and its error.
func (f *Fetcher) fetchTracked(ctx context.Context, feed *store.FetchFeed) (bool, error) {
	if _, busy := f.inflight.LoadOrStore(feed.ID, struct{}{}); busy {
		return false, nil
	}
	defer f.inflight.Delete(feed.ID)
	err := f.FetchFeed(ctx, feed)
	if err != nil {
		slog.Warn("feed fetch failed", "feed", feed.ID, "url", feed.URL, "err", err)
	}
	return true, err
}

// FetchNow refreshes the given feeds concurrently and waits for them (or
// for ctx). It returns how many failed.
func (f *Fetcher) FetchNow(ctx context.Context, feeds []*store.FetchFeed) int {
	sem := make(chan struct{}, f.cfg.FetchWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for _, feed := range feeds {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return failed
		}
		wg.Go(func() {
			defer func() { <-sem }()
			if _, err := f.fetchTracked(ctx, feed); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return failed
}

// Interval is the refresh interval for a user's choice in minutes: the
// server default (REFRESH_INTERVAL) when 0, never below the minimum.
func (f *Fetcher) Interval(minutes int) time.Duration {
	if minutes <= 0 {
		return f.cfg.RefreshInterval
	}
	return max(time.Duration(minutes)*time.Minute, config.MinRefresh)
}

func (f *Fetcher) nextFetch(feed *store.FetchFeed) int64 {
	d := f.Interval(feed.RefreshMinutes)
	if feed.Instant {
		d = max(d, instantPoll)
	}
	jitter := time.Duration(rand.Int64N(int64(d / 10)))
	return time.Now().Add(d + jitter).Unix()
}

func (f *Fetcher) backoff(feed *store.FetchFeed, errors int, retryAfter time.Duration) int64 {
	d := f.Interval(feed.RefreshMinutes)
	for i := 0; i < errors && d < maxBackoff; i++ {
		d *= 2
	}
	if retryAfter > d {
		d = retryAfter
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return time.Now().Add(d).Unix()
}

type httpError struct {
	status     int
	retryAfter time.Duration
}

func (e *httpError) Error() string {
	switch e.status {
	case http.StatusUnauthorized:
		return "HTTP 401: a username and password are needed (feed menu → Sign-in)"
	case http.StatusForbidden:
		return "HTTP 403: access denied"
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

// BasicAuth is the value of an HTTP Basic Authorization header.
func BasicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// FetchFeed polls a single feed and stores new articles.
func (f *Fetcher) FetchFeed(ctx context.Context, feed *store.FetchFeed) error {
	err := f.fetchFeed(ctx, feed)
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		// Stopped from outside (shutdown): not the feed's fault, so no error
		// is recorded and no backoff applies; it is fetched again soon.
		return err
	}
	if err != nil {
		retry := time.Duration(0)
		if he, ok := errors.AsType[*httpError](err); ok {
			retry = he.retryAfter
		}
		f.store.FeedFailed(feed.ID, friendlyError(err), f.backoff(feed, feed.ErrorCount+1, retry))
		return err
	}
	if !feed.HasIcon && time.Since(time.Unix(feed.IconCheckedAt, 0)) > iconRecheck {
		f.updateIcon(ctx, feed)
	}
	return nil
}

func (f *Fetcher) fetchFeed(ctx context.Context, feed *store.FetchFeed) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	headers := map[string]string{
		"Accept": "application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5",
	}
	// Validators are only useful once we already have the content.
	if feed.LastFetchedAt > 0 && feed.ErrorCount == 0 {
		headers["If-None-Match"] = feed.ETag
		headers["If-Modified-Since"] = feed.LastModified
	}
	if feed.AuthUser != "" {
		// net/http drops Authorization on redirects to another host.
		headers["Authorization"] = BasicAuth(feed.AuthUser, feed.AuthPass)
	}
	resp, err := netsafe.Get(ctx, f.client, feed.URL, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return f.store.FeedNotModified(feed.ID, f.nextFetch(feed))
	}
	if resp.StatusCode != http.StatusOK {
		return &httpError{status: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	body, err := netsafe.ReadLimited(resp.Body, maxFeedSize)
	if err != nil {
		return err
	}
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid feed: %w", err)
	}

	feedBase := resp.Request.URL
	siteURL := ""
	if parsed.Link != "" {
		siteURL = content.ResolveURL(feedBase, parsed.Link)
		if !content.IsHTTPURL(siteURL) {
			siteURL = ""
		}
	}
	cutoff := time.Now().AddDate(0, 0, -f.cfg.RetentionDays).Unix()
	// Sanitizing and ad filtering are the expensive part: skip the items
	// already stored (most of them on every poll).
	known, err := f.store.KnownGUIDs(feed.ID)
	if err != nil {
		return err
	}
	rules, err := f.store.FilterRules(feed.UserID)
	if err != nil {
		return err
	}
	items := make([]store.NewArticle, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		if h := quickGUIDHash(it, feedBase, siteURL); h != "" && known[h] {
			continue
		}
		if a, ok := buildArticle(it, feedBase, siteURL, cutoff); ok {
			a.HiddenBy, a.Read = filter.Decide(rules, feed.ID, feed.FolderID, a.Title, a.Text)
			items = append(items, a)
		}
	}
	added, err := f.store.InsertArticles(feed.ID, feed.UserID, feed.FullText, items)
	if err != nil {
		return err
	}
	if added > 0 && feed.FullText {
		f.WakeFullText()
	}
	if f.websub != nil && feed.AuthUser == "" {
		f.websub.Seen(feed.URL, resp.Header, body, feedBase)
	}
	title := strings.TrimSpace(content.PlainText(parsed.Title))
	if siteURL != "" {
		feed.SiteURL = siteURL
	}
	return f.store.FeedFetched(feed.ID, title, siteURL, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), f.nextFetch(feed))
}

// itemLink resolves the item link and the base URL for its content.
func itemLink(it *gofeed.Item, feedBase *url.URL, siteURL string) (link string, base *url.URL) {
	base = feedBase
	if siteURL != "" {
		if u := content.ParseBase(siteURL); u != nil {
			base = u
		}
	}
	if it.Link != "" {
		link = content.ResolveURL(base, it.Link)
		if !content.IsHTTPURL(link) {
			link = ""
		}
	}
	if link != "" {
		if u := content.ParseBase(link); u != nil {
			base = u
		}
	}
	return link, base
}

func hashGUID(guid string) string {
	sum := sha256.Sum256([]byte(guid))
	return hex.EncodeToString(sum[:16])
}

// quickGUIDHash is the GUID hash buildArticle would compute, when it does
// not depend on the content. Empty means "unknown, build the article".
func quickGUIDHash(it *gofeed.Item, feedBase *url.URL, siteURL string) string {
	if g := strings.TrimSpace(it.GUID); g != "" {
		return hashGUID(g)
	}
	if link, _ := itemLink(it, feedBase, siteURL); link != "" {
		return hashGUID(link)
	}
	return ""
}

func buildArticle(it *gofeed.Item, feedBase *url.URL, siteURL string, cutoff int64) (store.NewArticle, bool) {
	link, base := itemLink(it, feedBase, siteURL)

	now := time.Now()
	pub := now
	if it.PublishedParsed != nil {
		pub = *it.PublishedParsed
	} else if it.UpdatedParsed != nil {
		pub = *it.UpdatedParsed
	}
	if pub.After(now.Add(time.Hour)) {
		pub = now
	}
	if pub.Unix() < cutoff {
		return store.NewArticle{}, false
	}

	raw := it.Content
	if strings.TrimSpace(raw) == "" {
		raw = it.Description
	}
	clean := content.Sanitize(raw, base)
	text := content.PlainText(clean)

	title := strings.TrimSpace(it.Title)
	if strings.ContainsAny(title, "<&") {
		title = content.PlainText(title)
	}
	if title == "" {
		title = content.Truncate(text, 90)
	}
	if title == "" {
		title = "(untitled)"
	}

	guid := strings.TrimSpace(it.GUID)
	if guid == "" {
		guid = link
	}
	if guid == "" {
		guid = title + "|" + strconv.FormatInt(pub.Unix(), 10)
	}
	author := ""
	if len(it.Authors) > 0 && it.Authors[0] != nil {
		author = it.Authors[0].Name
	}
	return store.NewArticle{
		GUIDHash:    hashGUID(guid),
		URL:         link,
		Title:       content.Truncate(title, 500),
		Author:      content.Truncate(author, 200),
		Content:     clean,
		Preview:     content.Truncate(text, 280),
		Thumbnail:   thumbnail(it, clean, base),
		PublishedAt: pub.Unix(),
		Text:        text,
	}, true
}

func thumbnail(it *gofeed.Item, clean string, base *url.URL) string {
	check := func(s string) string {
		s = content.ResolveURL(base, s)
		if content.IsHTTPURL(s) {
			return s
		}
		return ""
	}
	if it.Image != nil {
		if u := check(it.Image.URL); u != "" {
			return u
		}
	}
	if media, ok := it.Extensions["media"]; ok {
		for _, key := range []string{"thumbnail", "content", "group"} {
			for _, e := range media[key] {
				if key == "group" {
					for _, sub := range []string{"thumbnail", "content"} {
						for _, c := range e.Children[sub] {
							if isImageMedia(sub, c.Attrs) {
								if u := check(c.Attrs["url"]); u != "" {
									return u
								}
							}
						}
					}
					continue
				}
				if isImageMedia(key, e.Attrs) {
					if u := check(e.Attrs["url"]); u != "" {
						return u
					}
				}
			}
		}
	}
	for _, enc := range it.Enclosures {
		if enc != nil && strings.HasPrefix(enc.Type, "image/") {
			if u := check(enc.URL); u != "" {
				return u
			}
		}
	}
	return content.FirstImage(clean)
}

func isImageMedia(kind string, attrs map[string]string) bool {
	if kind == "thumbnail" {
		return true
	}
	return attrs["medium"] == "image" || strings.HasPrefix(attrs["type"], "image/")
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func friendlyError(err error) string {
	var he *httpError
	switch {
	case errors.As(err, &he):
		switch he.status {
		case 401, 403:
			return fmt.Sprintf("access denied (HTTP %d)", he.status)
		case 404, 410:
			return fmt.Sprintf("feed not found (HTTP %d)", he.status)
		case 429:
			return "too many requests (HTTP 429)"
		}
		return he.Error()
	case errors.Is(err, netsafe.ErrBlocked):
		return "address not allowed (private network)"
	case errors.Is(err, netsafe.ErrTooLarge):
		return "feed too large"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return err.Error()
}
