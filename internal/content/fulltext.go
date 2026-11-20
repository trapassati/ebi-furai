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
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	resp, err := netsafe.Get(ctx, client, pageURL, map[string]string{
		"Accept": "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5",
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the site answered %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "html") {
		return "", errors.New("the page is not HTML")
	}
	body, err := netsafe.ReadLimited(resp.Body, maxPageSize)
	if err != nil {
		return "", err
	}
	reader, err := charset.NewReader(bytes.NewReader(body), ct)
	if err != nil {
		return "", err
	}
	page, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	final := resp.Request.URL
	// Remove ad blocks before extraction, while classes and ids are intact.
	if f := AdFilter(); f != nil {
		page = f.CleanDocument(page, final.Hostname())
	}
	article, err := readability.FromReader(bytes.NewReader(page), final)
	if err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}
	var out bytes.Buffer
	if err := article.RenderHTML(&out); err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}
	clean := Sanitize(out.String(), final)
	// Very short results are usually cookie walls or bot challenges.
	if len([]rune(strings.TrimSpace(PlainText(clean)))) < 250 {
		return "", errors.New("no readable content found")
	}
	return clean, nil
}

// ParseBase parses a URL used as base for relative links; nil on failure.
func ParseBase(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}
