package feeds

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"time"

	"golang.org/x/net/html/charset"

	"github.com/trapassati/ebi-furai/internal/store"
)

type opmlDoc struct {
	XMLName xml.Name    `xml:"opml"`
	Version string      `xml:"version,attr"`
	Head    opmlHead    `xml:"head"`
	Body    opmlOutline `xml:"body"`
}

type opmlHead struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated,omitempty"`
}

type opmlOutline struct {
	Text     string        `xml:"text,attr,omitempty"`
	Title    string        `xml:"title,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string        `xml:"htmlUrl,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline"`
}

// OPMLEntry is a feed read from an OPML file.
type OPMLEntry struct {
	Title  string
	URL    string
	Folder string
}

// ParseOPML returns the feeds of an OPML document. Nested folders are
// flattened to their top-level folder.
func ParseOPML(r io.Reader) ([]OPMLEntry, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false
	var doc opmlDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.New("invalid OPML file")
	}
	var out []OPMLEntry
	var walk func(o opmlOutline, folder string, depth int)
	walk = func(o opmlOutline, folder string, depth int) {
		if o.XMLURL != "" {
			title := o.Title
			if title == "" {
				title = o.Text
			}
			out = append(out, OPMLEntry{Title: strings.TrimSpace(title), URL: strings.TrimSpace(o.XMLURL), Folder: folder})
			return
		}
		name := folder
		if depth == 1 {
			name = strings.TrimSpace(o.Text)
			if name == "" {
				name = strings.TrimSpace(o.Title)
			}
		}
		for _, c := range o.Outlines {
			walk(c, name, depth+1)
		}
	}
	for _, o := range doc.Body.Outlines {
		walk(o, "", 1)
	}
	return out, nil
}

// BuildOPML renders the user's subscriptions grouped by folder.
func BuildOPML(title string, list []store.ExportFeed) ([]byte, error) {
	doc := opmlDoc{Version: "2.0", Head: opmlHead{Title: title, DateCreated: time.Now().UTC().Format(time.RFC1123Z)}}
	folders := map[string]int{}
	for _, f := range list {
		o := opmlOutline{Text: f.Title, Title: f.Title, Type: "rss", XMLURL: f.URL, HTMLURL: f.SiteURL}
		if f.Folder == "" {
			doc.Body.Outlines = append(doc.Body.Outlines, o)
			continue
		}
		idx, ok := folders[f.Folder]
		if !ok {
			doc.Body.Outlines = append(doc.Body.Outlines, opmlOutline{Text: f.Folder, Title: f.Folder})
			idx = len(doc.Body.Outlines) - 1
			folders[f.Folder] = idx
		}
		doc.Body.Outlines[idx].Outlines = append(doc.Body.Outlines[idx].Outlines, o)
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
