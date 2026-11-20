// Package dedup builds the keys that recognize the same article published
// by two feeds, and counts words for the reading time.
package dedup

import (
	"net/url"
	"strings"

	"github.com/trapassati/ebi-furai/internal/filter"
)

// WordsPerMinute is a calm reading speed for Italian and English prose.
const WordsPerMinute = 230

// Minimum words for a title to be compared: short ones ("Editoriale",
// "Le notizie del giorno") repeat without being the same article.
const minTitleWords = 4

// tracking query parameters, dropped from the address before comparing.
var tracking = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "msclkid": true, "igshid": true, "mc_cid": true, "mc_eid": true,
	"ref": true, "ref_src": true, "source": true, "src": true, "cmpid": true, "ncid": true, "ito": true, "rss": true,
}

// URLKey is the address without scheme, "www.", fragment, tracking
// parameters and trailing slash; "" when it cannot identify an article
// (a home page, for instance).
func URLKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" {
		return ""
	}
	q := u.Query()
	for k := range q {
		if tracking[strings.ToLower(k)] || strings.HasPrefix(strings.ToLower(k), "utm_") {
			q.Del(k)
		}
	}
	key := host + path
	if e := q.Encode(); e != "" {
		key += "?" + e
	}
	return key
}

// TitleKeys returns the normalized title and, when it ends with " - Name"
// or " | Name" (as Google News does: "Titolo - La Stampa"), the title
// without that part. Both are "" for titles too short to compare.
func TitleKeys(title string) (full, stripped string) {
	full = filter.Normalize(title)
	if len(strings.Fields(full)) < minTitleWords {
		full = ""
	}
	for _, sep := range []string{" - ", " – ", " — ", " | "} {
		i := strings.LastIndex(title, sep)
		if i <= 0 {
			continue
		}
		suffix := title[i+len(sep):]
		if n := len(strings.Fields(suffix)); n == 0 || n > 5 {
			continue
		}
		s := filter.Normalize(title[:i])
		if len(strings.Fields(s)) >= minTitleWords && s != full {
			stripped = s
		}
		break
	}
	return full, stripped
}

// Words counts the words of a plain text.
func Words(text string) int {
	return len(strings.Fields(text))
}

// Minutes is the reading time for a number of words (0 below a minute).
func Minutes(words int) int {
	if words < WordsPerMinute*3/4 {
		return 0
	}
	return max(1, (words+WordsPerMinute/2)/WordsPerMinute)
}
