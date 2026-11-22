// Package update checks now and then whether a newer release of Ebi Furai
// has been published, so that the app can tell the administrators.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the latest release of the official repository.
const DefaultURL = "https://api.github.com/repos/trapassati/ebi-furai/releases/latest"

const (
	firstCheck = 30 * time.Second
	every      = 12 * time.Hour
	retry      = time.Hour
)

// Status is what the app shows.
type Status struct {
	Current     string    `json:"current"`
	Latest      string    `json:"latest,omitempty"`
	Available   bool      `json:"available"`
	Notes       string    `json:"notes,omitempty"` // Markdown, without the install section
	URL         string    `json:"url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitzero"`
	CheckedAt   time.Time `json:"checked_at,omitzero"`
	Install     string    `json:"install"` // "docker", "service" or "" (unknown)
}

// Checker asks GitHub for the latest release every 12 hours.
type Checker struct {
	current string
	url     string
	client  *http.Client

	mu     sync.Mutex
	status Status

	// OnAvailable is called after a check that finds a newer release (set
	// before Run): the server notifies the administrators.
	OnAvailable func(Status)
}

// New returns a checker for this version ("1.2.0", or "dev" for a build
// from source, which is never told about updates).
func New(current, url string) *Checker {
	if url == "" {
		url = DefaultURL
	}
	return &Checker{
		current: current,
		url:     url,
		client:  &http.Client{Timeout: 20 * time.Second},
		status:  Status{Current: current, Install: InstallKind()},
	}
}

// Run checks shortly after start, then every 12 hours (every hour after a
// failure), until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) {
	wait := firstCheck
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		if err := c.Check(ctx); err != nil {
			slog.Warn("update check failed", "err", err)
			wait = retry
		}
	}
}

// Status returns the last result.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

type release struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

// Check asks for the latest release now.
func (c *Checker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Ebi-Furai/"+c.current)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if rel.Draft || rel.Prerelease || parse(latest) == nil {
		return fmt.Errorf("unexpected release %q", rel.TagName)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.Latest = latest
	c.status.Available = Newer(latest, c.current)
	c.status.Notes = notes(rel.Body)
	c.status.URL = rel.HTMLURL
	c.status.PublishedAt = rel.PublishedAt
	c.status.CheckedAt = time.Now()
	st := c.status
	if st.Available && c.OnAvailable != nil {
		go c.OnAvailable(st)
	}
	return nil
}

// notes drops the "Install" section of the release notes: the app shows
// the command for the way this server was installed.
func notes(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if i := strings.Index(body, "\n### Install"); i >= 0 {
		body = body[:i]
	}
	if len(body) > 20000 {
		body = body[:20000]
	}
	return strings.TrimSpace(body)
}

// parse reads "1.2.3" (a "-rc1" suffix is ignored).
func parse(v string) []int {
	v, _, _ = strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// Newer reports whether version a is newer than b. A build from source
// ("dev") is never behind.
func Newer(a, b string) bool {
	x, y := parse(a), parse(b)
	if x == nil || y == nil {
		return false
	}
	for i := range 3 {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// InstallKind tells how this server runs, for the update command to show:
// the Docker image sets EBI_FURAI_INSTALL=docker; systemd sets
// INVOCATION_ID for its services.
func InstallKind() string {
	switch v := os.Getenv("EBI_FURAI_INSTALL"); v {
	case "docker", "service":
		return v
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	if os.Getenv("INVOCATION_ID") != "" {
		return "service"
	}
	return ""
}
