package content

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html/charset"

	"github.com/trapassati/ebi-furai/internal/netsafe"
)

const maxPageSize = 5 << 20

// FetchFullText downloads a page through the SSRF-safe client and extracts
// the readable content, already sanitized.
func FetchFullText(ctx context.Context, client *http.Client, pageURL string) (string, error) {
	p, err := FetchPage(ctx, client, pageURL)
	if err != nil {
		return "", err
	}
	// Very short results are usually cookie walls or bot challenges.
	if len([]rune(strings.TrimSpace(PlainText(p.HTML)))) < 250 {
		return "", errors.New("no readable content found")
	}
	return p.HTML, nil
}

// Page is a web page reduced to its readable content.
type Page struct {
	URL      string // after redirects
	Title    string
	Byline   string
	Excerpt  string
	Image    string
	SiteName string
	HTML     string // sanitized; may be short or empty
}

// FetchPage downloads a page through the SSRF-safe client and extracts its
// title, author, picture and readable content.
func FetchPage(ctx context.Context, client *http.Client, pageURL string) (*Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	resp, err := netsafe.Get(ctx, client, pageURL, map[string]string{
		"Accept": "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the site answered %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "html") {
		return nil, errors.New("the page is not HTML")
	}
	body, err := netsafe.ReadLimited(resp.Body, maxPageSize)
	if err != nil {
		return nil, err
	}
	reader, err := charset.NewReader(bytes.NewReader(body), ct)
	if err != nil {
		return nil, err
	}
	page, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	final := resp.Request.URL
	// Remove ad blocks before extraction, while classes and ids are intact.
	if f := AdFilter(); f != nil {
		page = f.CleanDocument(page, final.Hostname())
	}
	article, err := readability.FromReader(bytes.NewReader(page), final)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w", err)
	}
	out := &Page{
		URL:      final.String(),
		Title:    collapseSpace(article.Title()),
		Byline:   collapseSpace(article.Byline()),
		Excerpt:  collapseSpace(article.Excerpt()),
		SiteName: collapseSpace(article.SiteName()),
	}
	if img := article.ImageURL(); img != "" {
		if u := ResolveURL(final, img); IsHTTPURL(u) {
			out.Image = u
		}
	}
	if article.Node != nil {
		var buf bytes.Buffer
		if err := article.RenderHTML(&buf); err != nil {
			return nil, fmt.Errorf("extraction failed: %w", err)
		}
		out.HTML = Sanitize(buf.String(), final)
	}
	return out, nil
}

// ParseBase parses a URL used as base for relative links; nil on failure.
func ParseBase(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}
