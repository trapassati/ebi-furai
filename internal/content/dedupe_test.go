package content

import (
	"strings"
	"testing"
)

func TestRewriteDropsDuplicateImages(t *testing.T) {
	s := NewImageSigner([]byte("k"), false)
	cases := []struct {
		name, in string
		want     int
	}{
		{"same url", `<p><img src="https://x.it/a/photo-basilica.jpg"></p><p><img src="https://x.it/a/photo-basilica.jpg?w=800"></p>`, 1},
		{"adjacent resized", `<figure><img src="https://x.it/1200/photo-basilica.jpg"></figure><figure><img src="https://x.it/800/photo-basilica-800x600.jpg"></figure>`, 1},
		{"same name after text", `<img src="https://x.it/1200/photo-basilica.jpg"><p>testo</p><img src="https://x.it/800/photo-basilica.jpg">`, 2},
		{"different images", `<img src="https://x.it/a.jpg"><img src="https://x.it/b.jpg">`, 2},
		{"query images", `<img src="https://x.it/img.php?id=1"><img src="https://x.it/img.php?id=2">`, 2},
	}
	for _, c := range cases {
		out := s.Rewrite(c.in)
		if got := strings.Count(out, "<img"); got != c.want {
			t.Errorf("%s: %d images, want %d\n%s", c.name, got, c.want, out)
		}
	}
	out := s.Rewrite(`<figure><a href="https://x.it"><img src="https://x.it/p/lead-photo.jpg"></a></figure><figure><a href="https://x.it"><img src="https://x.it/p/lead-photo.jpg"></a></figure><p>ok</p>`)
	if strings.Count(out, "<figure") != 1 || strings.Count(out, "<a ") != 1 {
		t.Errorf("empty wrappers not removed: %s", out)
	}
}
