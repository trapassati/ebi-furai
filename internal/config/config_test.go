package config

import "testing"

func TestPublicURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "",
		" https://reader.example.com/ ":  "https://reader.example.com",
		"http://192.168.1.5:9099":        "http://192.168.1.5:9099",
		"https://example.com/ebi-furai/": "https://example.com/ebi-furai",
	} {
		t.Setenv("PUBLIC_URL", in)
		c, err := Load()
		if err != nil {
			t.Errorf("%q: %v", in, err)
		} else if c.PublicURL != want {
			t.Errorf("%q: got %q, want %q", in, c.PublicURL, want)
		}
	}
	for _, bad := range []string{"reader.example.com", "ftp://example.com", "https://u:p@example.com", "https://example.com/?a=1"} {
		t.Setenv("PUBLIC_URL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
