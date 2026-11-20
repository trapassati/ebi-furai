package api

import (
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

// aboutModule is a Go module compiled into the server, for the credits.
type aboutModule struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	License string `json:"license"` // SPDX identifier, empty when not known
	URL     string `json:"url"`     // the module's licenses on pkg.go.dev
}

// moduleLicenses holds the licenses of the modules the server uses, directly
// or not. The list of modules itself comes from the binary, so it is always
// the one really compiled; a module missing here is still listed, with the
// link to its licenses.
var moduleLicenses = map[string]string{
	"codeberg.org/readeck/go-readability/v2": "MIT",
	"github.com/go-chi/chi/v5":               "MIT",
	"github.com/microcosm-cc/bluemonday":     "BSD-3-Clause",
	"github.com/mmcdole/gofeed":              "MIT",
	"github.com/mmcdole/goxpp":               "MIT",
	"github.com/mmcdole/goxpp/v2":            "MIT",
	"github.com/PuerkitoBio/goquery":         "BSD-3-Clause",
	"github.com/andybalholm/cascadia":        "BSD-2-Clause",
	"github.com/araddon/dateparse":           "MIT",
	"github.com/itlightning/dateparse":       "MIT",
	"github.com/aymerick/douceur":            "MIT",
	"github.com/gorilla/css":                 "BSD-3-Clause",
	"github.com/go-shiori/dom":               "MIT",
	"github.com/gogs/chardet":                "MIT",
	"github.com/json-iterator/go":            "MIT",
	"github.com/modern-go/concurrent":        "Apache-2.0",
	"github.com/modern-go/reflect2":          "Apache-2.0",
	"github.com/dustin/go-humanize":          "MIT",
	"github.com/google/uuid":                 "BSD-3-Clause",
	"github.com/mattn/go-isatty":             "MIT",
	"github.com/ncruces/go-strftime":         "MIT",
	"github.com/remyoudompheng/bigfft":       "BSD-3-Clause",
	"modernc.org/sqlite":                     "BSD-3-Clause",
	"modernc.org/libc":                       "BSD-3-Clause",
	"modernc.org/mathutil":                   "BSD-3-Clause",
	"modernc.org/memory":                     "BSD-3-Clause",
}

func licenseOf(path string) string {
	if l, ok := moduleLicenses[path]; ok {
		return l
	}
	if strings.HasPrefix(path, "golang.org/x/") {
		return "BSD-3-Clause"
	}
	return ""
}

var buildModules = sync.OnceValue(func() []aboutModule {
	list := []aboutModule{}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return list
	}
	for _, d := range info.Deps {
		// A replaced module keeps its name; a version replacement wins.
		version := d.Version
		if d.Replace != nil && d.Replace.Version != "" {
			version = d.Replace.Version
		}
		list = append(list, aboutModule{
			Path:    d.Path,
			Version: version,
			License: licenseOf(d.Path),
			URL:     "https://pkg.go.dev/" + d.Path + "@" + version + "?tab=licenses",
		})
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Path) < strings.ToLower(list[j].Path) })
	return list
})

// Release is the version of this build ("1.2.0"), set by main from the
// linker flags of the release builds; "dev" otherwise.
var Release = "dev"

// handleAbout returns what the "Informazioni" page shows: version, where
// the source code is (AGPL-3.0, section 13) and the modules compiled in.
func (s *Server) handleAbout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    s.static.Version,
		"release":    Release,
		"go":         runtime.Version(),
		"source_url": s.cfg.SourceURL,
		"modules":    buildModules(),
	})
}
