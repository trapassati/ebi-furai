package content

import (
	"strings"
	"testing"
)

// 9to5Mac ends every article with a link to Google's "preferred source"
// page holding a light and a dark badge.
func TestRewriteDropsPreferredSourceBadges(t *testing.T) {
	s := NewImageSigner([]byte("k"), false)
	in := `<p>The update follows <a href="https://9to5mac.com/x">Things 3.24</a>.</p>` +
		`<p><a href="https://google.com/preferences/source?q=https://9to5mac.com">` +
		`<img src="https://9to5mac.com/wp-content/themes/ninetofive/dist/images/google-preferred-source-badge-dark.png" alt="Add 9to5Mac as a preferred source on Google">` +
		`<img src="https://9to5mac.com/wp-content/themes/ninetofive/dist/images/google-preferred-source-badge-light.png" alt="Add 9to5Mac as a preferred source on Google"></a></p>` +
		`<img src="https://other.example/img/google-preferred-source-badge.png">` +
		`<p><a href="https://www.google.com/search?q=things">Google search</a> <img src="https://x.it/photo.jpg"></p>`
	out := s.Rewrite(in)
	for _, gone := range []string{"preferences/source", "preferred-source"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s still there:\n%s", gone, out)
		}
	}
	if strings.Count(out, "<p>") != 2 {
		t.Errorf("the empty paragraph should go, the others stay:\n%s", out)
	}
	for _, kept := range []string{"Things 3.24", "google.com/search", "photo.jpg"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%s removed:\n%s", kept, out)
		}
	}
}
