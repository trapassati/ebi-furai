// Package filter matches articles against keyword filters: whole words or
// phrases, case and accent insensitive; a trailing * matches the words
// that start with it ("calcio*" also matches "calciomercato").
package filter

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	MaxTerms   = 50
	MaxTermLen = 80
)

// Normalize lowercases, removes accents and turns everything that is not
// a letter or a digit into single spaces.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// ParseTerms splits what the user typed (comma or newline separated) into
// normalized terms, without duplicates. A trailing * is kept.
func ParseTerms(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, chunk := range raw {
		for _, part := range strings.FieldsFunc(chunk, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
			part = strings.TrimSpace(part)
			prefix := strings.HasSuffix(part, "*")
			t := Normalize(strings.TrimRight(part, "*"))
			if t == "" {
				continue
			}
			if len(t) > MaxTermLen {
				t = strings.TrimSpace(t[:MaxTermLen])
			}
			if prefix {
				t += "*"
			}
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Rule is one filter, ready to match.
type Rule struct {
	ID       int64
	Terms    []string
	Action   string // "hide" or "read"
	FeedID   int64  // 0 = any
	FolderID int64  // 0 = any
	InBody   bool
}

// Applies reports whether the rule covers a feed.
func (r *Rule) Applies(feedID, folderID int64) bool {
	switch {
	case r.FeedID != 0:
		return r.FeedID == feedID
	case r.FolderID != 0:
		return r.FolderID == folderID
	}
	return true
}

// Match reports whether title (or the text, with InBody) contains one of
// the terms. Both are raw: they are normalized here.
func (r *Rule) Match(title, text string) bool {
	if matchTerms(r.Terms, " "+Normalize(title)+" ") {
		return true
	}
	return r.InBody && text != "" && matchTerms(r.Terms, " "+Normalize(text)+" ")
}

// MatchNormalized is Match with title and text already padded and
// normalized (" " + Normalize(s) + " "), to check many rules at once.
func (r *Rule) MatchNormalized(title, text string) bool {
	return matchTerms(r.Terms, title) || (r.InBody && matchTerms(r.Terms, text))
}

func matchTerms(terms []string, padded string) bool {
	for _, t := range terms {
		if p, ok := strings.CutSuffix(t, "*"); ok {
			if strings.Contains(padded, " "+p) {
				return true
			}
		} else if strings.Contains(padded, " "+t+" ") {
			return true
		}
	}
	return false
}

// Pad normalizes a text for MatchNormalized.
func Pad(s string) string { return " " + Normalize(s) + " " }

// Decide picks what happens to an article among the rules that apply to
// its feed: hiding wins over marking as read. It returns the hiding rule id
// (0 when none) and whether the article is read.
func Decide(rules []*Rule, feedID, folderID int64, title, text string) (hiddenBy int64, read bool) {
	if len(rules) == 0 {
		return 0, false
	}
	pt, px := Pad(title), ""
	for _, r := range rules {
		if !r.Applies(feedID, folderID) {
			continue
		}
		if r.InBody && px == "" {
			px = Pad(text)
		}
		if !r.MatchNormalized(pt, px) {
			continue
		}
		if r.Action == "hide" {
			return r.ID, true
		}
		read = true
	}
	return 0, read
}
