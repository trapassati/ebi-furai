package store

import (
	"slices"
	"testing"
)

// The same story from two feeds: the second copy arrives read, the lists
// outside its feed show only the first, and reading one reads the other.
func TestDuplicates(t *testing.T) {
	st, uid, paper := newTestStore(t)
	gnews, _ := st.CreateFeed(uid, "https://news.google.com/rss", "Google News", "", 0)
	st.InsertArticles(paper, uid, false, []NewArticle{
		{GUIDHash: "p1", Title: "Data center in orbita alimentati dal Sole", URL: "https://www.lastampa.it/tech/data-center/", PublishedAt: 10, Text: "uno due tre"},
		{GUIDHash: "p2", Title: "Un altro articolo del giornale di oggi", URL: "https://www.lastampa.it/altro", PublishedAt: 11},
	})
	st.InsertArticles(gnews, uid, false, []NewArticle{
		{GUIDHash: "g1", Title: "Data center in orbita alimentati dal Sole - La Stampa", URL: "https://news.google.com/rss/articles/XYZ", PublishedAt: 12},
		{GUIDHash: "g2", Title: "Una notizia che il giornale non ha", URL: "https://news.google.com/rss/articles/ABC", PublishedAt: 13},
		{GUIDHash: "g3", Title: "Il link uguale anche se il titolo cambia", URL: "http://lastampa.it/altro?utm_source=gn", PublishedAt: 14},
	})
	id := func(guid string) int64 {
		var x int64
		st.DB.QueryRow(`SELECT id FROM articles WHERE guid_hash = ?`, guid).Scan(&x)
		return x
	}
	unread, _, _ := st.ListArticles(Selection{UserID: uid, Kind: "unread"}, ListOptions{})
	if len(unread) != 3 {
		t.Fatalf("unread shows each story once: %d", len(unread))
	}
	all, _, _ := st.ListArticles(Selection{UserID: uid, Kind: "all"}, ListOptions{})
	if len(all) != 3 {
		t.Fatalf("all shows each story once: %d", len(all))
	}
	inFeed, _, _ := st.ListArticles(Selection{UserID: uid, Kind: "feed", ID: gnews}, ListOptions{})
	if len(inFeed) != 3 || !inFeed[2].Read {
		t.Fatalf("the duplicate is in its feed, read: %+v", inFeed)
	}
	// Reading the copy in Google News reads the newspaper's one.
	st.SetRead(uid, id("g1"), false)
	st.SetRead(uid, id("g1"), true)
	also, err := st.ReadDuplicates(uid, id("g1"))
	if err != nil || !slices.Equal(also, []int64{id("p1")}) {
		t.Fatalf("also read %v %v", also, err)
	}
	// Off: nothing is matched.
	st.SetDedup(uid, false)
	st.InsertArticles(gnews, uid, false, []NewArticle{{GUIDHash: "g4", Title: "Un altro articolo del giornale di oggi", URL: "https://x.example/a", PublishedAt: 15}})
	var dup any
	st.DB.QueryRow(`SELECT dup_of FROM articles WHERE guid_hash = 'g4'`).Scan(&dup)
	if dup != nil {
		t.Fatal("with the option off duplicates are not marked")
	}
	// Deleting the newspaper frees its copies.
	st.DeleteFeed(uid, paper)
	all, _, _ = st.ListArticles(Selection{UserID: uid, Kind: "all"}, ListOptions{})
	if len(all) != 4 {
		t.Fatalf("after deleting the first copy the others show again: %d", len(all))
	}
}

func TestMarkRange(t *testing.T) {
	st, uid, fid := newTestStore(t)
	var items []NewArticle
	for i := 1; i <= 10; i++ {
		items = append(items, NewArticle{GUIDHash: string(rune('a' + i)), Title: "x", PublishedAt: int64(i * 10)})
	}
	st.InsertArticles(fid, uid, false, items)
	var pivot int64
	st.DB.QueryRow(`SELECT id FROM articles WHERE published_at = 50`).Scan(&pivot)
	sel := Selection{UserID: uid, Kind: "unread"}
	max := st.MaxArticleID(uid)
	// Newest first: above = 60..100.
	ids, err := st.MarkRange(sel, ListOptions{}, 50, pivot, true, max)
	if err != nil || len(ids) != 5 {
		t.Fatalf("above, newest first: %v %v", ids, err)
	}
	st.MarkUnread(uid, ids)
	// Oldest first: above = 10..40.
	ids, _ = st.MarkRange(sel, ListOptions{Oldest: true}, 50, pivot, true, max)
	if len(ids) != 4 {
		t.Fatalf("above, oldest first: %v", ids)
	}
	// Below, newest first = older = 10..40, already read: nothing.
	ids, _ = st.MarkRange(sel, ListOptions{}, 50, pivot, false, max)
	if len(ids) != 0 {
		t.Fatalf("below: %v", ids)
	}
	var unread int
	st.DB.QueryRow(`SELECT COUNT(*) FROM articles WHERE read = 0`).Scan(&unread)
	if unread != 6 {
		t.Fatalf("the article itself and the others stay unread: %d", unread)
	}
}

// The offline copy reads full articles with the same columns as the lists.
func TestGetArticlesForOffline(t *testing.T) {
	st, uid, fid := newTestStore(t)
	st.InsertArticles(fid, uid, false, []NewArticle{{GUIDHash: "a", Title: "A", PublishedAt: 1, Text: "uno due tre"}})
	var id int64
	st.DB.QueryRow(`SELECT id FROM articles`).Scan(&id)
	list, err := st.GetArticles(uid, []int64{id})
	if err != nil || len(list) != 1 || list[0].Words != 3 {
		t.Fatalf("GetArticles: %v %+v", err, list)
	}
}
