package dedup

import "testing"

func TestURLKey(t *testing.T) {
	same := []string{
		"https://www.lastampa.it/cronaca/2026/11/20/news/articolo-123/",
		"http://lastampa.it/cronaca/2026/11/20/news/articolo-123?utm_source=rss&utm_medium=feed",
		"https://lastampa.it/cronaca/2026/11/20/news/articolo-123#commenti",
		"https://lastampa.it/cronaca/2026/11/20/news/articolo-123?ref=rss&fbclid=x",
	}
	want := URLKey(same[0])
	if want != "lastampa.it/cronaca/2026/11/20/news/articolo-123" {
		t.Fatalf("key %q", want)
	}
	for _, u := range same[1:] {
		if got := URLKey(u); got != want {
			t.Errorf("URLKey(%q) = %q, want %q", u, got, want)
		}
	}
	if URLKey("https://example.com/?p=1") != "" || URLKey("https://example.com/") != "" || URLKey("not a url") != "" {
		t.Error("home pages and junk give no key")
	}
	if URLKey("https://example.com/post?id=1") == URLKey("https://example.com/post?id=2") {
		t.Error("real query parameters are kept")
	}
}

func TestTitleKeys(t *testing.T) {
	f, s := TitleKeys("Data center in orbita alimentati dal Sole - La Stampa")
	if f != "data center in orbita alimentati dal sole la stampa" || s != "data center in orbita alimentati dal sole" {
		t.Fatalf("got %q %q", f, s)
	}
	f2, _ := TitleKeys("Data center in orbita, alimentati dal Sole")
	if f2 != s {
		t.Fatalf("the newspaper's title %q should match the stripped one %q", f2, s)
	}
	if f, s := TitleKeys("Editoriale"); f != "" || s != "" {
		t.Error("short titles are not compared")
	}
	if _, s := TitleKeys("Inter - Milan"); s != "" {
		t.Error("a short part before the dash is not a title")
	}
	if _, s := TitleKeys("Un titolo abbastanza lungo - con una parte dopo il trattino che non è una testata giornalistica"); s != "" {
		t.Error("long suffixes are part of the title")
	}
}

func TestMinutes(t *testing.T) {
	for words, want := range map[int]int{0: 0, 100: 0, 171: 0, 172: 1, 400: 2, 920: 4, 2300: 10} {
		if got := Minutes(words); got != want {
			t.Errorf("Minutes(%d) = %d, want %d", words, got, want)
		}
	}
}
