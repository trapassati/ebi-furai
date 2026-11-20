// Package content sanitizes article HTML and extracts text, previews and
// thumbnails.
package content

import (
	"bytes"
	"net/url"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"

	"github.com/trapassati/ebi-furai/internal/adblock"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowElements("figure", "figcaption", "picture", "mark", "small", "sub", "sup")
	return p
}()

// AdFilter returns the active ad filter (nil when off). Set by main.
var AdFilter = func() *adblock.Filter { return nil }

// Sanitize resolves relative URLs against base, removes ads and anything
// unsafe. Ads go first: element hiding needs the class and id attributes
// that the sanitizer drops.
func Sanitize(raw string, base *url.URL) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	resolved := raw
	site := ""
	if base != nil {
		resolved = resolveURLs(raw, base)
		site = base.Hostname()
	}
	if f := AdFilter(); f != nil {
		resolved = f.CleanFragment(resolved, site, true)
	}
	return strings.TrimSpace(policy.Sanitize(resolved))
}

func resolveURLs(raw string, base *url.URL) string {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return raw
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for i, a := range n.Attr {
				switch a.Key {
				case "href", "src", "poster":
					n.Attr[i].Val = ResolveURL(base, a.Val)
				case "srcset":
					n.Attr[i].Val = "" // dropped later anyway; avoids unresolved candidates
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		walk(n)
		html.Render(&buf, n)
	}
	return buf.String()
}

// ResolveURL returns ref resolved against base, or ref unchanged on error.
func ResolveURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || base == nil {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

// PlainText converts HTML to whitespace-normalized plain text.
func PlainText(raw string) string {
	z := html.NewTokenizer(strings.NewReader(raw))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return collapseSpace(b.String())
		case html.StartTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "noscript", "template":
				skip++
			case "p", "div", "br", "li", "h1", "h2", "h3", "h4", "h5", "h6", "tr", "blockquote", "figcaption":
				b.WriteByte(' ')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "noscript", "template":
				if skip > 0 {
					skip--
				}
			case "p", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6", "td", "th":
				b.WriteByte(' ')
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
}

func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// Truncate shortens s to at most n runes, adding an ellipsis.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := strings.TrimRightFunc(string(r[:n]), unicode.IsSpace)
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)*3/4 {
		cut = cut[:i]
	}
	return cut + "…"
}

// FirstImage returns the first plausible image URL found in sanitized HTML.
func FirstImage(raw string) string {
	z := html.NewTokenizer(strings.NewReader(raw))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return ""
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "img" || !hasAttr {
			continue
		}
		var src, w, h string
		for {
			k, v, more := z.TagAttr()
			switch string(k) {
			case "src":
				src = string(v)
			case "width":
				w = string(v)
			case "height":
				h = string(v)
			}
			if !more {
				break
			}
		}
		if w == "1" || h == "1" || w == "0" || h == "0" {
			continue // tracking pixel
		}
		if IsHTTPURL(src) {
			return src
		}
	}
}

// IsHTTPURL reports whether s is an absolute http(s) URL.
func IsHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
