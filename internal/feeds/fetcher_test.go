package feeds

import (
	"net/url"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
)

// quickGUIDHash must match the hash stored by buildArticle, or known
// items would be rebuilt (or new ones skipped).
func TestQuickGUIDHashMatchesBuild(t *testing.T) {
	base, _ := url.Parse("https://site.it/feed.xml")
	items := []*gofeed.Item{
		{GUID: " tag:site.it,2026:1 ", Title: "A", Link: "/a"},
		{Title: "B", Link: "/articoli/b"},
		{Title: "C"}, // no guid, no link: not skippable
	}
	for _, it := range items {
		a, ok := buildArticle(it, base, "https://site.it/", 0)
		if !ok {
			t.Fatal("not built")
		}
		q := quickGUIDHash(it, base, "https://site.it/")
		if it.GUID == "" && it.Link == "" {
			if q != "" {
				t.Errorf("%s: expected no quick hash", it.Title)
			}
			continue
		}
		if q != a.GUIDHash {
			t.Errorf("%s: quick %s != built %s", it.Title, q, a.GUIDHash)
		}
	}
}

func TestFriendlyError401(t *testing.T) {
	err := &httpError{status: 401}
	if m := friendlyError(err, false); !strings.Contains(m, "asks for a username and password") || !strings.Contains(m, "Sign-in with password") {
		t.Errorf("without sign-in: %q", m)
	}
	if m := friendlyError(err, true); !strings.Contains(m, "not accepted") {
		t.Errorf("with sign-in: %q", m)
	}
	if m := friendlyError(&httpError{status: 403}, false); m != "access denied (HTTP 403)" {
		t.Errorf("403: %q", m)
	}
}
