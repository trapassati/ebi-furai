package content

import (
	"net/url"
	"strings"
)

// clutter reports whether a link or image is a site's self-promotion that
// has no place in a reader, even with the ad filter off: today the "Add as
// a preferred source on Google" badges that many news sites append to
// every article (9to5Mac, for one).
func clutter(link, img string) bool {
	if link != "" {
		if u, err := url.Parse(link); err == nil {
			host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
			if strings.HasPrefix(host, "google.") && strings.HasPrefix(u.Path, "/preferences/source") {
				return true
			}
		}
	}
	if img != "" {
		if u, err := url.Parse(img); err == nil {
			name := strings.ToLower(u.Path[strings.LastIndexByte(u.Path, '/')+1:])
			if strings.Contains(name, "preferred-source") || strings.Contains(name, "preferred_source") {
				return true
			}
		}
	}
	return false
}
