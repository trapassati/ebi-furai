package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.2.0", "1.1.0", true},
		{"1.10.0", "1.9.3", true},
		{"2.0.0", "1.99.99", true},
		{"1.1.0", "1.1.0", false},
		{"1.0.9", "1.1.0", false},
		{"1.2.0", "dev", false},
		{"garbage", "1.0.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "Ebi-Furai/1.1.0" && ua != "Ebi-Furai/dev" {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://example.org/r","published_at":"2026-11-21T10:00:00Z",
			"body":"- New thing\r\n- Fixed thing\r\n\r\n### Install\r\nDocker: ..."}`))
	}))
	defer srv.Close()
	c := New("1.1.0", srv.URL)
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := c.Status()
	if !st.Available || st.Latest != "1.2.0" || st.Notes != "- New thing\n- Fixed thing" || st.URL != "https://example.org/r" {
		t.Fatalf("status %+v", st)
	}
	dev := New("dev", srv.URL)
	dev.Check(context.Background())
	if dev.Status().Available {
		t.Fatal("a build from source is told about updates")
	}
}
