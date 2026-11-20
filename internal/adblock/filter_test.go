package adblock

import (
	"strings"
	"testing"
)

const list = `[Adblock Plus 2.0]
! Title: test
||doubleclick.net^
||ads.example.com^$third-party
||cdn.example.org/banners/
||tracker.it^$domain=foo.it
@@||ok.doubleclick.net^
##.ad-banner
###sponsor
##div.adbox
##a[href^="http://x"]
repubblica.it##.taboola
~skip.it,foo.it##.nope
#@#.ad-banner-ok
`

func filter(t *testing.T) *Filter {
	f := New()
	if err := f.Add(strings.NewReader(list)); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBlockURL(t *testing.T) {
	f := filter(t)
	for u, want := range map[string]bool{
		"https://ad.doubleclick.net/x.gif":      true,
		"https://ok.doubleclick.net/x.gif":      false,
		"https://ads.example.com/a.png":         true,
		"https://example.com/a.png":             false,
		"https://cdn.example.org/banners/1.jpg": true,
		"https://cdn.example.org/photos/1.jpg":  false,
		"https://tracker.it/p.gif":              false, // domain= option skipped
	} {
		if got := f.BlockURL(u); got != want {
			t.Errorf("%s: %v, want %v", u, got, want)
		}
	}
}

func TestCleanFragment(t *testing.T) {
	f := filter(t)
	in := `<p>Testo</p><div class="ad-banner">ADV</div><div id="sponsor">S</div><div class="adbox">B</div>` +
		`<span class="adbox">keep span</span><a href="https://ad.doubleclick.net/c"><img src="https://ad.doubleclick.net/i.gif"></a>` +
		`<p><img src="https://example.com/photo.jpg"></p><div class="taboola">T</div>`
	out := f.CleanFragment(in, "www.repubblica.it", true)
	for _, gone := range []string{"ADV", ">S<", ">B<", "doubleclick", ">T<"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q not removed: %s", gone, out)
		}
	}
	for _, kept := range []string{"Testo", "keep span", "photo.jpg"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q removed: %s", kept, out)
		}
	}
	if out2 := f.CleanFragment(`<div class="taboola">T</div>`, "other.it", true); !strings.Contains(out2, ">T<") {
		t.Errorf("site rule applied to another site: %s", out2)
	}
	if f.Removed() != 5 {
		t.Errorf("removed = %d", f.Removed())
	}
}
