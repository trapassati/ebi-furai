// Package adblock removes ads from article HTML using EasyList-format
// filter lists. Only the rule kinds that are safe to apply outside a
// browser are used:
//
//   - network rules anchored to a domain (||ads.example.com^ and
//     ||example.com/ads/), applied to images, frames, media and links;
//   - element hiding rules made of a single class or id, optionally with
//     a tag (##.ad-banner, ###sponsor, ##div.adbox), generic or for given
//     sites, with their #@# exceptions.
//
// Rules with wildcards, regexps, attribute selectors or option lists that
// depend on the browser context are skipped.
package adblock

import (
	"bufio"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
)

type Filter struct {
	block    map[string]bool            // blocked hosts (and their subdomains)
	allow    map[string]bool            // @@||host^ exceptions
	paths    map[string][]string        // host -> blocked path prefixes
	hide     map[string]bool            // generic selectors: ".c", "#i", "div.c"
	unhide   map[string]bool            // generic #@# exceptions
	siteHide map[string]map[string]bool // site -> selectors
	Rules    int
	removed  *atomic.Int64
}

func New() *Filter {
	return &Filter{
		block: map[string]bool{}, allow: map[string]bool{}, paths: map[string][]string{},
		hide: map[string]bool{}, unhide: map[string]bool{}, siteHide: map[string]map[string]bool{},
		removed: new(atomic.Int64),
	}
}

// Removed is the number of elements removed since the filter was built.
func (f *Filter) Removed() int64 { return f.removed.Load() }

var (
	reNetDomain = regexp.MustCompile(`^\|\|([a-z0-9][a-z0-9.-]*\.[a-z]{2,})\^?(?:\$(.*))?$`)
	reNetPath   = regexp.MustCompile(`^\|\|([a-z0-9][a-z0-9.-]*\.[a-z]{2,})(/[A-Za-z0-9._/-]+)\^?(?:\$(.*))?$`)
	reSelector  = regexp.MustCompile(`^([a-z][a-z0-9]*)?([.#][A-Za-z_][A-Za-z0-9_-]*)$`)
)

// Options that do not narrow a rule to a browser context we cannot see.
var okOptions = map[string]bool{
	"third-party": true, "3p": true, "image": true, "script": true, "subdocument": true,
	"media": true, "object": true, "xmlhttprequest": true, "other": true, "all": true, "important": true,
}

func optionsOK(opts string) bool {
	if opts == "" {
		return true
	}
	for _, o := range strings.Split(opts, ",") {
		if !okOptions[strings.TrimSpace(o)] {
			return false
		}
	}
	return true
}

// Add parses one list and merges its rules into f.
func (f *Filter) Add(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		f.addLine(strings.TrimSpace(sc.Text()))
	}
	return sc.Err()
}

func (f *Filter) addLine(line string) {
	if line == "" || line[0] == '!' || line[0] == '[' {
		return
	}
	// element hiding
	if i := strings.Index(line, "##"); i >= 0 {
		sites, sel := line[:i], line[i+2:]
		if !reSelector.MatchString(sel) {
			return
		}
		if sites == "" {
			f.hide[sel] = true
			f.Rules++
			return
		}
		if strings.Contains(sites, "~") {
			return
		}
		for _, s := range strings.Split(sites, ",") {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" || strings.ContainsAny(s, "*/") {
				continue
			}
			if f.siteHide[s] == nil {
				f.siteHide[s] = map[string]bool{}
			}
			f.siteHide[s][sel] = true
		}
		f.Rules++
		return
	}
	if strings.HasPrefix(line, "#@#") {
		if sel := line[3:]; reSelector.MatchString(sel) {
			f.unhide[sel] = true
		}
		return
	}
	if strings.Contains(line, "#") { // #?#, #$#, #@$#... scriptlets and procedural rules
		return
	}
	// network rules
	exception := strings.HasPrefix(line, "@@")
	line = strings.TrimPrefix(line, "@@")
	if m := reNetDomain.FindStringSubmatch(line); m != nil {
		if !optionsOK(m[2]) {
			return
		}
		if exception {
			f.allow[m[1]] = true
		} else {
			f.block[m[1]] = true
			f.Rules++
		}
		return
	}
	if m := reNetPath.FindStringSubmatch(line); m != nil && !exception {
		if !optionsOK(m[3]) {
			return
		}
		f.paths[m[1]] = append(f.paths[m[1]], m[2])
		f.Rules++
	}
}

// suffixes yields host and its parent domains: a.b.c.it, b.c.it, c.it.
func suffixes(host string, fn func(string) bool) bool {
	for {
		if fn(host) {
			return true
		}
		i := strings.IndexByte(host, '.')
		if i < 0 || strings.IndexByte(host[i+1:], '.') < 0 {
			return false
		}
		host = host[i+1:]
	}
}

// BlockURL reports whether raw points to an ad or tracking server.
func (f *Filter) BlockURL(raw string) bool {
	if f == nil {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if suffixes(host, func(h string) bool { return f.allow[h] }) {
		return false
	}
	if suffixes(host, func(h string) bool { return f.block[h] }) {
		return true
	}
	return suffixes(host, func(h string) bool {
		for _, p := range f.paths[h] {
			if strings.HasPrefix(u.Path, p) {
				return true
			}
		}
		return false
	})
}
