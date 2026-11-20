package content

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Pages often carry the same picture twice: a lazy-load placeholder plus a
// <noscript> copy, a <picture> fallback, or the lead image repeated by the
// full-text extraction. deduper drops an image when its URL (without query)
// was already seen, or when it has the same file name as the image right
// before it with no text in between (same photo, different size/CDN path).
type deduper struct {
	seen      map[string]bool
	lastName  string
	textSince bool
}

func newDeduper() *deduper { return &deduper{seen: map[string]bool{}} }

// text records that visible text appeared since the last image.
func (d *deduper) text() { d.textSince = true }

var (
	imageExt = regexp.MustCompile(`(?i)\.(jpe?g|png|gif|webp|avif)$`)
	sizeTag  = regexp.MustCompile(`(?i)([-_@]\d{2,4}x\d{0,4}|[-_@]\d{2,4}w|[-_@]\dx|[-_](scaled|large|medium|small|thumb))+$`)
)

// imageKeys returns the exact key (host + path, plus the query when the path
// does not look like an image file) and a size-insensitive file name.
func imageKeys(raw string) (exact, name string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	file := path.Base(u.Path)
	exact = host + u.Path
	if !imageExt.MatchString(file) {
		if u.RawQuery != "" {
			exact += "?" + u.RawQuery
		}
		return exact, ""
	}
	base := strings.ToLower(strings.TrimSuffix(file, path.Ext(file)))
	base = sizeTag.ReplaceAllString(base, "")
	if len(base) < 6 {
		return exact, "" // too generic ("image", "0") to compare
	}
	return exact, host + "/" + base
}

// duplicate reports whether the image src repeats an earlier one.
func (d *deduper) duplicate(src string) bool {
	exact, name := imageKeys(src)
	if exact == "" {
		return false
	}
	dup := d.seen[exact] || (!d.textSince && name != "" && name == d.lastName)
	if !dup {
		d.seen[exact] = true
		d.lastName = name
		d.textSince = false
	}
	return dup
}

// removeImage detaches n and then any wrapper left without content
// (figure, picture, link, paragraph...). Top-level nodes are dropped from
// the returned slice.
func removeImage(top []*html.Node, n *html.Node) []*html.Node {
	for {
		parent := n.Parent
		if parent != nil {
			parent.RemoveChild(n)
		} else {
			for i, t := range top {
				if t == n {
					top = append(top[:i], top[i+1:]...)
					break
				}
			}
			return top
		}
		if !emptyWrapper(parent) {
			return top
		}
		n = parent
	}
}

func emptyWrapper(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Figure, atom.Picture, atom.A, atom.P, atom.Div, atom.Span:
	default:
		return false
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if strings.TrimSpace(c.Data) != "" {
				return false
			}
		case html.ElementNode:
			if c.DataAtom != atom.Source && c.DataAtom != atom.Br {
				return false
			}
		}
	}
	return true
}
