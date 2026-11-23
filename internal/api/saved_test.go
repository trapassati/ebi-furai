package api

import "testing"

func TestSaveURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a#top":                         "https://example.com/a",
		"  http://example.com/b \n":                         "http://example.com/b",
		"A title https://example.com/c?u=http://x.org/y":    "https://example.com/c?u=http://x.org/y",
		"Read this: HTTPS://Example.com/d and that":         "https://Example.com/d",
		"https://example.com/e?q=http://other.example/page": "https://example.com/e?q=http://other.example/page",
	}
	for in, want := range cases {
		if got, ok := saveURL(in); !ok || got != want {
			t.Errorf("saveURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "not a link", "ftp://example.com/x", "javascript:alert(1)", "https://"} {
		if got, ok := saveURL(bad); ok {
			t.Errorf("saveURL(%q) = %q, want refused", bad, got)
		}
	}
}
