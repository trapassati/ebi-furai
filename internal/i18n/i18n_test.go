package i18n

import (
	"net/http/httptest"
	"testing"
)

func TestT(t *testing.T) {
	cases := map[string]string{
		"invalid credentials":                        "credenziali non valide",
		"feed not found (HTTP 404)":                  "feed non trovato (HTTP 404)",
		"full text not available: timeout":           "testo completo non disponibile: tempo scaduto",
		"the file has too many feeds (at most 2000)": "il file contiene troppi feed (massimo 2000)",
		"something from a library":                   "something from a library",
	}
	for en, want := range cases {
		if got := T(Italian, en); got != want {
			t.Errorf("T(%q) = %q, want %q", en, got, want)
		}
		if got := T(English, en); got != en {
			t.Errorf("English changed %q", en)
		}
	}
}

func TestFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "de-DE,it;q=0.8,en;q=0.5")
	if FromRequest(r) != Italian {
		t.Error("Accept-Language")
	}
	r.Header.Set("X-Lang", "en")
	if FromRequest(r) != English {
		t.Error("X-Lang wins")
	}
}
