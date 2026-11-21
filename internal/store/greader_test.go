package store

import (
	"fmt"
	"testing"
)

func TestAPIStreams(t *testing.T) {
	st, uid, fid := newTestStore(t)
	var items []NewArticle
	for i := range 30 {
		items = append(items, NewArticle{GUIDHash: fmt.Sprint("api", i), Title: fmt.Sprint("a", i), PublishedAt: int64(1000 + i)})
	}
	if _, err := st.InsertArticles(fid, uid, false, items); err != nil {
		t.Fatal(err)
	}
	all := func(s APIStream, oldest bool, page int) []int64 {
		ids := []int64{}
		cont := ""
		for {
			list, next, err := st.APIItemIDs(s, oldest, cont, page)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range list {
				ids = append(ids, r.ID)
			}
			if next == "" {
				return ids
			}
			cont = next
		}
	}
	base := APIStream{UserID: uid}
	if n := len(all(base, false, 7)); n < 30 {
		t.Fatalf("paging lost articles: %d", n)
	}
	ids := all(base, true, 100)
	// Read the oldest 10, star 2.
	if err := st.SetReadIDs(uid, ids[:10], true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStarredIDs(uid, ids[:2], true); err != nil {
		t.Fatal(err)
	}
	unread := base
	unread.Unread = true
	starred := base
	starred.Starred = true
	before := len(all(unread, false, 100))
	if got := len(all(starred, false, 100)); got != 2 {
		t.Fatalf("starred: %d", got)
	}
	// Another user cannot touch them.
	if err := st.SetReadIDs(uid+99, ids[10:], true); err != nil {
		t.Fatal(err)
	}
	if got := len(all(unread, false, 100)); got != before {
		t.Fatalf("another user read articles: %d → %d", before, got)
	}
	items2, err := st.APIItems(uid, []int64{ids[1], ids[0], ids[1], 999999})
	if err != nil || len(items2) != 2 || items2[0].ID != ids[1] || !items2[0].Starred || !items2[0].Read {
		t.Fatalf("items %+v %v", items2, err)
	}
	if other, _ := st.APIItems(uid+99, ids); len(other) != 0 {
		t.Fatal("another user's articles were returned")
	}
	// Mark as read up to a date: only the older ones.
	cutoff := int64(1000 + 20)
	if _, err := st.APIMarkAllRead(APIStream{UserID: uid, FeedID: fid}, cutoff); err != nil {
		t.Fatal(err)
	}
	left := all(unread, false, 100)
	for _, id := range left {
		it, _ := st.APIItems(uid, []int64{id})
		if it[0].PublishedAt <= cutoff {
			t.Fatalf("article of %d still unread", it[0].PublishedAt)
		}
	}
	if len(left) == 0 {
		t.Fatal("newer articles were marked read too")
	}
	counts, _ := st.APIUnreadCounts(uid)
	if len(counts) != 1 || counts[0].Count != len(left) {
		t.Fatalf("unread counts %+v, want %d", counts, len(left))
	}
}

func TestAppPasswords(t *testing.T) {
	st, uid, _ := newTestStore(t)
	p, err := st.CreateAppPassword(uid, "Reeder", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	st.DB.QueryRow(`SELECT username FROM users WHERE id = ?`, uid).Scan(&name)
	if got, pid, err := st.AppPasswordLogin(name, "hash1"); err != nil || got != uid || pid != p.ID {
		t.Fatalf("login: %d %d %v", got, pid, err)
	}
	if _, _, err := st.AppPasswordLogin(name, "wrong"); err != ErrNotFound {
		t.Fatalf("wrong secret: %v", err)
	}
	for i := range 12 {
		if err := st.CreateAPIToken(fmt.Sprint("tok", i), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if u, _, err := st.APIUser("tok11"); err != nil || u.ID != uid {
		t.Fatalf("token: %v", err)
	}
	if _, _, err := st.APIUser("tok0"); err != ErrNotFound {
		t.Fatal("old tokens are not pruned")
	}
	st.DB.Exec(`UPDATE users SET disabled = 1 WHERE id = ?`, uid)
	if _, _, err := st.APIUser("tok11"); err != ErrNotFound {
		t.Fatal("a disabled user can use the API")
	}
	st.DB.Exec(`UPDATE users SET disabled = 0 WHERE id = ?`, uid)
	if err := st.DeleteAppPassword(uid, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.APIUser("tok11"); err != ErrNotFound {
		t.Fatal("revoking the app password keeps its apps signed in")
	}
}
