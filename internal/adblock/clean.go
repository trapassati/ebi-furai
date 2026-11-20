package adblock

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// hidden reports whether an element matches an element hiding rule for
// the given site (host of the page or feed the HTML comes from).
func (f *Filter) hidden(n *html.Node, site string) bool {
	match := func(sel string) bool {
		if f.unhide[sel] {
			return false
		}
		if f.hide[sel] {
			return true
		}
		return site != "" && suffixes(site, func(h string) bool { return f.siteHide[h][sel] })
	}
	for _, a := range n.Attr {
		switch a.Key {
		case "id":
			if id := strings.TrimSpace(a.Val); id != "" && (match("#"+id) || match(n.Data+"#"+id)) {
				return true
			}
		case "class":
			for _, c := range strings.Fields(a.Val) {
				if match("."+c) || match(n.Data+"."+c) {
					return true
				}
			}
		}
	}
	return false
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// blockedNode reports whether the element loads or links to an ad server.
func (f *Filter) blockedNode(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Img, atom.Iframe, atom.Embed, atom.Video, atom.Audio, atom.Script, atom.Source:
		return f.BlockURL(attrVal(n, "src")) || f.BlockURL(attrVal(n, "data-src"))
	case atom.Object:
		return f.BlockURL(attrVal(n, "data"))
	case atom.A:
		return f.BlockURL(attrVal(n, "href"))
	}
	return false
}

// prune removes matching elements below root and returns how many.
func (f *Filter) prune(root *html.Node, site string, cosmetic bool) int {
	var drop []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (f.blockedNode(c) || (cosmetic && f.hidden(c, site))) {
				drop = append(drop, c)
				continue
			}
			walk(c)
		}
	}
	walk(root)
	for _, n := range drop {
		n.Parent.RemoveChild(n)
	}
	f.removed.Add(int64(len(drop)))
	return len(drop)
}

// CleanFragment filters an HTML fragment (feed item content). URLs must
// already be absolute. cosmetic enables the element hiding rules, which
// need the original class and id attributes.
func (f *Filter) CleanFragment(raw, site string, cosmetic bool) string {
	if f == nil || raw == "" {
		return raw
	}
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(raw), body)
	if err != nil {
		return raw
	}
	for _, n := range nodes {
		body.AppendChild(n)
	}
	if f.prune(body, strings.ToLower(site), cosmetic) == 0 {
		return raw
	}
	var buf bytes.Buffer
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		html.Render(&buf, c)
	}
	return buf.String()
}

// CleanDocument filters a whole page before the readable text is extracted.
func (f *Filter) CleanDocument(page []byte, site string) []byte {
	if f == nil {
		return page
	}
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return page
	}
	if f.prune(doc, strings.ToLower(site), true) == 0 {
		return page
	}
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return page
	}
	return buf.Bytes()
}
