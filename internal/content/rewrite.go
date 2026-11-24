package content

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ImageSigner builds and verifies signed image-proxy URLs, so the proxy
// only fetches URLs that the server itself produced.
type ImageSigner struct {
	key     []byte
	Enabled bool
}

func NewImageSigner(key []byte, enabled bool) *ImageSigner {
	return &ImageSigner{key: key, Enabled: enabled}
}

func (s *ImageSigner) sign(raw string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

// Verify checks that sig matches raw.
func (s *ImageSigner) Verify(raw, sig string) bool {
	return hmac.Equal([]byte(s.sign(raw)), []byte(sig))
}

// URL returns the URL to use in the page for an external image: a signed
// same-origin proxy URL, or the original https URL when the proxy is off.
func (s *ImageSigner) URL(raw string) string {
	if !IsHTTPURL(raw) {
		return ""
	}
	if !s.Enabled {
		if after, ok := strings.CutPrefix(raw, "http://"); ok {
			return "https://" + after
		}
		return raw
	}
	return "/img?u=" + url.QueryEscape(raw) + "&s=" + s.sign(raw)
}

// Rewrite prepares stored (already sanitized) HTML for display: images go
// through the proxy and load lazily, links open outside the app.
func (s *ImageSigner) Rewrite(raw string) string {
	if raw == "" {
		return ""
	}
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(raw), body)
	if err != nil {
		return ""
	}
	dd := newDeduper()
	var dupes []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
			dd.text()
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Img, atom.Iframe, atom.A, atom.Video, atom.Audio, atom.Embed:
				// Stored articles are filtered again here, so the ad
				// filter also applies to content saved before it.
				target := attr(n, "src")
				if n.DataAtom == atom.A {
					target = attr(n, "href")
				}
				if ad := AdFilter(); ad != nil && ad.BlockURL(target) {
					dupes = append(dupes, n)
					return
				}
				// Badges like "Add as a preferred source on Google" go too,
				// also from articles stored before this rule.
				if (n.DataAtom == atom.A && clutter(target, "")) || (n.DataAtom == atom.Img && clutter("", target)) {
					dupes = append(dupes, n)
					return
				}
			}
			switch n.DataAtom {
			case atom.Img:
				if dd.duplicate(attr(n, "src")) {
					dupes = append(dupes, n)
					return
				}
				attrs := n.Attr[:0]
				for _, a := range n.Attr {
					switch a.Key {
					case "srcset", "sizes", "loading", "decoding":
						continue
					case "src":
						a.Val = s.URL(a.Val)
					}
					attrs = append(attrs, a)
				}
				n.Attr = append(attrs,
					html.Attribute{Key: "loading", Val: "lazy"},
					html.Attribute{Key: "decoding", Val: "async"})
			case atom.Source:
				// <picture><source srcset> would bypass the proxy.
				n.Attr = nil
			case atom.A:
				setAttr(n, "target", "_blank")
				setAttr(n, "rel", "noopener noreferrer")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	// Remove duplicates in a second pass (the walk iterates siblings).
	for _, n := range dupes {
		nodes = removeImage(nodes, n)
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		html.Render(&buf, n)
	}
	return buf.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}
