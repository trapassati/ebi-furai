// Package i18n translates the server's messages (errors, notifications)
// into the user's language. The code writes them in English; it.go holds
// the Italian. Messages with values in them ("feed not found (HTTP 404)")
// are matched against the patterns of the catalog.
package i18n

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// Languages of the interface.
const (
	English = "en"
	Italian = "it"
)

type pattern struct {
	re *regexp.Regexp
	it string // Italian format, with %s for every value
}

var patterns []pattern

func init() {
	verb := regexp.MustCompile(`%[sdqv]`)
	for en, itf := range it {
		if !verb.MatchString(en) {
			continue
		}
		parts := verb.Split(en, -1)
		for i := range parts {
			parts[i] = regexp.QuoteMeta(parts[i])
		}
		patterns = append(patterns, pattern{
			re: regexp.MustCompile("^" + strings.Join(parts, "(.+?)") + "$"),
			it: verb.ReplaceAllString(itf, "%s"),
		})
	}
}

// Normalize returns a supported language for a code like "it-IT" or "en".
func Normalize(code string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(code)), "it") {
		return Italian
	}
	return English
}

// FromRequest picks the language of a request: the app's X-Lang header,
// otherwise the browser's Accept-Language.
func FromRequest(r *http.Request) string {
	if v := r.Header.Get("X-Lang"); v != "" {
		return Normalize(v)
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		code := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.HasPrefix(code, "it") || strings.HasPrefix(code, "en") {
			return Normalize(code)
		}
	}
	return English
}

type ctxKey struct{}

// WithLang stores the language in a context.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// Lang returns the language stored in a context (English by default).
func Lang(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return English
}

// T translates an English message into lang. Unknown messages (from
// libraries, for instance) are returned as they are.
func T(lang, msg string) string {
	if lang != Italian || msg == "" {
		return msg
	}
	if s, ok := it[msg]; ok {
		return s
	}
	for _, p := range patterns {
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		out := p.it
		for _, v := range m[1:] {
			out = strings.Replace(out, "%s", T(lang, v), 1)
		}
		return out
	}
	return msg
}
