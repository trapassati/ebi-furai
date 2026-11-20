package feeds

import (
	"net/url"
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
