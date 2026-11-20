package filter

import (
	"slices"
	"testing"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"  Perché il CALCIO?!  ": "perche il calcio",
		"Serie-A: Juve–Inter":    "serie a juve inter",
		"Città  e   caffè":       "citta e caffe",
		"":                       "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTerms(t *testing.T) {
	got := ParseTerms([]string{"Calcio, serie A;  JUVE*\ncalcio", " , *"})
	want := []string{"calcio", "serie a", "juve*"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMatch(t *testing.T) {
	r := &Rule{Terms: []string{"calcio", "serie a", "juve*"}}
	cases := []struct {
		title string
		want  bool
	}{
		{"Il calcio italiano", true},
		{"CALCIO: le notizie", true},
		{"Calcium e vitamine", false},   // whole words only
		{"Calciomercato aperto", false}, // "calcio" has no *
		{"Lo scudetto della Serie A", true},
		{"La serie animata", false},
		{"Juventus, la formazione", true}, // juve*
		{"Il deja-vu della juve", true},
		{"Notizie dal mondo", false},
	}
	for _, c := range cases {
		if got := r.Match(c.title, ""); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.title, got, c.want)
		}
	}
	if r.Match("Notizie", "si parla di calcio") {
		t.Error("the text is ignored without InBody")
	}
	r.InBody = true
	if !r.Match("Notizie", "si parla di calcio") {
		t.Error("InBody matches the text")
	}
}

func TestDecide(t *testing.T) {
	rules := []*Rule{
		{ID: 1, Terms: []string{"oroscopo"}, Action: "read"},
		{ID: 2, Terms: []string{"calcio"}, Action: "hide", FolderID: 7},
		{ID: 3, Terms: []string{"meteo"}, Action: "hide", FeedID: 4},
	}
	type res struct {
		hidden int64
		read   bool
	}
	cases := []struct {
		feed, folder int64
		title        string
		want         res
	}{
		{1, 0, "Oroscopo di oggi", res{0, true}},
		{1, 7, "Calcio e oroscopo", res{2, true}}, // hide wins
		{1, 0, "Calcio", res{0, false}},           // other folder
		{4, 0, "Il meteo", res{3, true}},
		{5, 0, "Il meteo", res{0, false}},
	}
	for _, c := range cases {
		h, r := Decide(rules, c.feed, c.folder, c.title, "")
		if (res{h, r}) != c.want {
			t.Errorf("Decide(%d, %d, %q) = %v %v, want %v", c.feed, c.folder, c.title, h, r, c.want)
		}
	}
}
