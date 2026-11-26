package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

type staticFile struct {
	body  []byte
	ctype string
	etag  string
	cache string
}

// Static serves the embedded PWA. A few files are templates: the app name
// and the build version are injected at startup.
type Static struct {
	files   map[string]*staticFile
	Version string
	csp     string
}

// NewStatic loads all files under root from fsys.
func NewStatic(fsys fs.FS, root, appName string, imageProxy bool) (*Static, error) {
	sub, err := fs.Sub(fsys, root)
	if err != nil {
		return nil, err
	}
	raw := map[string][]byte{}
	var names []string
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		raw[p] = b
		names = append(names, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	h := sha256.New()
	h.Write([]byte(appName))
	for _, n := range names {
		h.Write([]byte(n))
		h.Write(raw[n])
	}
	s := &Static{files: map[string]*staticFile{}, Version: hex.EncodeToString(h.Sum(nil))[:10]}

	jsonName, _ := json.Marshal(appName)
	for _, n := range names {
		body := raw[n]
		switch n {
		case "index.html":
			body = replaceAll(body, "__APP_NAME__", html.EscapeString(appName), "__VERSION__", s.Version)
		case "manifest.webmanifest":
			body = replaceAll(body, `"__APP_NAME__"`, string(jsonName), "__VERSION__", s.Version)
		case "sw.js":
			body = replaceAll(body, "__VERSION__", s.Version)
		}
		sum := sha256.Sum256(body)
		f := &staticFile{
			body:  body,
			ctype: contentType(n),
			etag:  `"` + hex.EncodeToString(sum[:8]) + `"`,
			cache: "no-cache",
		}
		// sprite.svg changes with the app, so it is revalidated like the JS.
		if (strings.HasPrefix(n, "icons/") && n != "icons/sprite.svg") || strings.HasPrefix(n, "splash/") || strings.HasPrefix(n, "fonts/") || strings.HasPrefix(n, "vendor/") {
			f.cache = "public, max-age=604800"
		}
		s.files[n] = f
	}

	imgSrc := "'self' data: blob:"
	if !imageProxy {
		imgSrc += " https:"
	}
	s.csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src " + imgSrc +
		"; connect-src 'self'; font-src 'self'; manifest-src 'self'; worker-src 'self'; media-src 'self' https:; " +
		// The YouTube player, loaded only when a video is tapped.
		"frame-src https://www.youtube-nocookie.com; " +
		"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	return s, nil
}

func replaceAll(b []byte, pairs ...string) []byte {
	s := string(b)
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, pairs[i], pairs[i+1])
	}
	return []byte(s)
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".webmanifest":
		return "application/manifest+json"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func (s *Static) serve(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.write(w, r, name)
	}
}

func (s *Static) write(w http.ResponseWriter, r *http.Request, name string) {
	f, ok := s.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.ctype)
	h.Set("Cache-Control", f.cache)
	h.Set("ETag", f.etag)
	if name == "index.html" {
		h.Set("Content-Security-Policy", s.csp)
	}
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	w.Write(f.body)
}

// Mount registers the static routes.
func (s *Static) Mount(r chi.Router) {
	r.Get("/", s.serve("index.html"))
	r.Get("/index.html", s.serve("index.html"))
	r.Get("/manifest.webmanifest", s.serve("manifest.webmanifest"))
	r.Get("/sw.js", s.serve("sw.js"))
	r.Get("/favicon.ico", s.serve("icons/favicon-32.png"))
	r.Get("/apple-touch-icon.png", s.serve("icons/apple-touch-icon.png"))
	r.Get("/apple-touch-icon-precomposed.png", s.serve("icons/apple-touch-icon.png"))
	r.Get("/static/*", func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(chi.URLParam(r, "*"))
		if name == "." || strings.HasPrefix(name, "..") || name == "index.html" {
			http.NotFound(w, r)
			return
		}
		s.write(w, r, name)
	})
}
