// Package websub keeps the feeds that name a WebSub hub subscribed to it,
// so the hub tells the server as soon as a site publishes (instant updates).
// A notification only makes the server fetch the feed right away: what it
// contains is never used, so every article still goes through the normal
// download, parsing and sanitizing.
package websub

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html/charset"
)

// Discover returns the hub and the self address that a feed declares, in
// its Link headers (preferred, as the specification says) or in the feed:
// <link rel="hub"> and <link rel="self"> (Atom, or atom:link in RSS) or
// "hubs" and "feed_url" (JSON Feed). Relative addresses are resolved against
// base; anything but http(s) is ignored.
func Discover(h http.Header, body []byte, base *url.URL) (hub, self string) {
	for _, v := range h.Values("Link") {
		for _, l := range parseLinkHeader(v) {
			for _, rel := range l.rels {
				switch {
				case rel == "hub" && hub == "":
					hub = l.target
				case rel == "self" && self == "":
					self = l.target
				}
			}
		}
	}
	if hub == "" || self == "" {
		var fh, fs string
		if t := bytes.TrimSpace(body); len(t) > 0 && t[0] == '{' {
			fh, fs = jsonLinks(t)
		} else {
			fh, fs = xmlLinks(body)
		}
		if hub == "" {
			hub = fh
		}
		if self == "" {
			self = fs
		}
	}
	return resolve(base, hub), resolve(base, self)
}

func resolve(base *url.URL, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

type link struct {
	target string
	rels   []string
}

// parseLinkHeader reads an HTTP Link header (RFC 8288):
// <https://hub.example/>; rel="hub", <https://site.example/feed>; rel=self
func parseLinkHeader(v string) []link {
	var out []link
	for {
		start := strings.IndexByte(v, '<')
		if start < 0 {
			return out
		}
		end := strings.IndexByte(v[start:], '>')
		if end < 0 {
			return out
		}
		l := link{target: v[start+1 : start+end]}
		v = v[start+end+1:]
		// Parameters, up to the comma before the next link.
		for {
			v = strings.TrimLeft(v, " \t")
			if v == "" || v[0] != ';' {
				break
			}
			v = strings.TrimLeft(v[1:], " \t")
			i := strings.IndexAny(v, "=;,")
			if i < 0 {
				i = len(v)
			}
			name := strings.ToLower(strings.TrimSpace(v[:i]))
			v = v[i:]
			val := ""
			if strings.HasPrefix(v, "=") {
				v = strings.TrimLeft(v[1:], " \t")
				if strings.HasPrefix(v, `"`) {
					if j := strings.IndexByte(v[1:], '"'); j >= 0 {
						val, v = v[1:1+j], v[2+j:]
					} else {
						val, v = v[1:], ""
					}
				} else {
					j := strings.IndexAny(v, ";,")
					if j < 0 {
						j = len(v)
					}
					val, v = strings.TrimSpace(v[:j]), v[j:]
				}
			}
			if name == "rel" {
				l.rels = append(l.rels, strings.Fields(strings.ToLower(val))...)
			}
		}
		out = append(out, l)
	}
}

// xmlLinks looks for the hub and self links of an RSS or Atom feed. They
// are in the feed's header: the scan stops at the first item.
func xmlLinks(body []byte) (hub, self string) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = charset.NewReaderLabel
	for {
		tok, err := dec.Token()
		if err != nil {
			return hub, self
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch strings.ToLower(se.Name.Local) {
		case "item", "entry":
			return hub, self
		case "link":
			var rel, href string
			for _, a := range se.Attr {
				switch strings.ToLower(a.Name.Local) {
				case "rel":
					rel = a.Value
				case "href":
					href = a.Value
				}
			}
			for _, r := range strings.Fields(strings.ToLower(rel)) {
				switch {
				case r == "hub" && hub == "":
					hub = href
				case r == "self" && self == "":
					self = href
				}
			}
			if hub != "" && self != "" {
				return hub, self
			}
		}
	}
}

// jsonLinks reads "hubs" and "feed_url" of a JSON Feed.
func jsonLinks(body []byte) (hub, self string) {
	var f struct {
		FeedURL string `json:"feed_url"`
		Hubs    []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"hubs"`
	}
	if json.Unmarshal(body, &f) != nil {
		return "", ""
	}
	for _, h := range f.Hubs {
		if t := strings.ToLower(h.Type); t == "websub" || t == "pubsubhubbub" {
			hub = h.URL
			break
		}
	}
	return hub, f.FeedURL
}
