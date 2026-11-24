package websub

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/store"
)

func sign(method, secret string, body []byte) string {
	var h func() hash.Hash
	switch strings.ToLower(method) {
	case "sha1":
		h = sha1.New
	case "sha384":
		h = sha512.New384
	case "sha512":
		h = sha512.New
	default:
		h = sha256.New
	}
	mac := hmac.New(h, []byte(secret))
	mac.Write(body)
	return method + "=" + hex.EncodeToString(mac.Sum(nil))
}

// fakeHub is a WebSub hub: it checks the callback before answering, as
// some real hubs do, and can then notify it.
type fakeHub struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	requests []url.Values
	deny     bool
	answers  []string // what the callback answered to each check
	secret   string
	callback string
	lease    string
}

func newFakeHub(t *testing.T) *fakeHub {
	h := &fakeHub{t: t, lease: "3600"}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.ParseForm() != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f := r.PostForm
		h.mu.Lock()
		h.requests = append(h.requests, f)
		if f.Get("hub.mode") == "subscribe" {
			h.secret, h.callback = f.Get("hub.secret"), f.Get("hub.callback")
		}
		deny := h.deny
		h.mu.Unlock()
		q := url.Values{"hub.topic": {f.Get("hub.topic")}}
		if deny {
			q.Set("hub.mode", "denied")
			q.Set("hub.reason", "not today")
		} else {
			q.Set("hub.mode", f.Get("hub.mode"))
			q.Set("hub.challenge", "challenge-"+f.Get("hub.mode"))
			q.Set("hub.lease_seconds", h.lease)
		}
		resp, err := http.Get(f.Get("hub.callback") + "?" + q.Encode())
		if err != nil {
			t.Errorf("hub: callback: %v", err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		h.mu.Lock()
		h.answers = append(h.answers, resp.Status+" "+string(b))
		h.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// notify posts a change to the callback, signed with secret.
func (h *fakeHub) notify(secret string) int {
	h.mu.Lock()
	callback := h.callback
	h.mu.Unlock()
	body := []byte(`<feed><entry><title>new</title></entry></feed>`)
	req, _ := http.NewRequest(http.MethodPost, callback, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/atom+xml")
	if secret != "" {
		req.Header.Set("X-Hub-Signature", sign("sha256", secret, body))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (h *fakeHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

func (h *fakeHub) request(i int) url.Values {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.requests[i]
}

func (h *fakeHub) answer(i int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.answers[i]
}

func (h *fakeHub) key() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.secret
}

func (h *fakeHub) refuse() {
	h.mu.Lock()
	h.deny = true
	h.mu.Unlock()
}

type env struct {
	st    *store.Store
	m     *Manager
	hub   *fakeHub
	wakes atomic.Int32
	users []int64
	feeds []int64
}

const feedURL = "https://site.example/feed.xml"

func setup(t *testing.T) *env {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	e := &env{st: store.New(conn), hub: newFakeHub(t)}
	// Two people follow the same feed: one subscription for both.
	for _, name := range []string{"anna", "bruno"} {
		res, err := conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at, lang) VALUES(?, 'x', 0, 0, 'it')`, name)
		if err != nil {
			t.Fatal(err)
		}
		uid, _ := res.LastInsertId()
		fid, err := e.st.CreateFeed(uid, feedURL, "F", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		e.users = append(e.users, uid)
		e.feeds = append(e.feeds, fid)
	}
	var m *Manager
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.Handle(w, r, strings.TrimPrefix(r.URL.Path, "/websub/"))
	}))
	t.Cleanup(cb.Close)
	m = New(e.st, cb.URL+"/", func() { e.wakes.Add(1) })
	m.client = &http.Client{Timeout: 5 * time.Second}
	m.checkURL = func(s string) (*url.URL, error) { return url.Parse(s) }
	e.m = m
	return e
}

func (e *env) seen(t *testing.T) {
	t.Helper()
	final, _ := url.Parse(feedURL)
	h := http.Header{"Link": {"<" + e.hub.srv.URL + "/>; rel=hub"}}
	e.m.Seen(feedURL, h, []byte(`<rss><channel><title>F</title></channel></rss>`), final)
}

func (e *env) sub(t *testing.T) *store.WebSub {
	t.Helper()
	w, err := e.st.WebSubByFeedURL(feedURL)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func (e *env) instant(t *testing.T, i int) bool {
	t.Helper()
	info, err := e.st.FeedInfo(e.users[i], e.feeds[i])
	if err != nil {
		t.Fatal(err)
	}
	ff, err := e.st.FetchFeedByID(e.users[i], e.feeds[i])
	if err != nil {
		t.Fatal(err)
	}
	if info.Instant != ff.Instant {
		t.Fatalf("instant: sources %v, fetcher %v", info.Instant, ff.Instant)
	}
	return info.Instant
}

func TestSubscribeNotifyUnsubscribe(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if e.instant(t, 0) {
		t.Fatal("instant before any subscription")
	}
	e.seen(t)
	e.seen(t) // the other user's fetch: same subscription
	e.m.check(ctx)
	e.m.check(ctx) // nothing more to do
	if n := e.hub.count(); n != 1 {
		t.Fatalf("%d requests to the hub, want 1", n)
	}
	r := e.hub.request(0)
	w := e.sub(t)
	if r.Get("hub.mode") != "subscribe" || r.Get("hub.topic") != feedURL || r.Get("hub.secret") != w.Secret ||
		r.Get("hub.callback") != e.m.Callback(w.Token) || r.Get("hub.lease_seconds") != "864000" {
		t.Fatalf("request %v", r)
	}
	if !strings.HasPrefix(e.m.Callback(w.Token), "http://127.0.0.1:") || strings.Contains(e.m.Callback(w.Token), "//websub") {
		t.Fatalf("callback %s", e.m.Callback(w.Token))
	}
	if a := e.hub.answer(0); a != "200 OK challenge-subscribe" {
		t.Fatalf("callback answered %q", a)
	}
	if w.Pending != "" || w.LeaseSeconds != 3600 || w.VerifiedUntil < time.Now().Unix()+3500 {
		t.Fatalf("subscription %+v", w)
	}
	if !e.instant(t, 0) || !e.instant(t, 1) {
		t.Fatal("not instant after the hub confirmed")
	}

	// Notifications: only a correct signature makes the feeds due.
	e.st.DB.Exec(`UPDATE feeds SET next_fetch_at = ?, last_fetched_at = ?`, time.Now().Unix()+3*3600, time.Now().Unix()-60)
	for _, bad := range []string{"", "wrong-secret"} {
		if code := e.hub.notify(bad); code != http.StatusAccepted {
			t.Fatalf("unsigned notification: HTTP %d", code)
		}
	}
	if e.wakes.Load() != 0 {
		t.Fatal("a notification without the right signature woke the fetcher")
	}
	if code := e.hub.notify(e.hub.key()); code != http.StatusAccepted {
		t.Fatalf("notification: HTTP %d", code)
	}
	if e.wakes.Load() != 1 {
		t.Fatal("the fetcher was not woken")
	}
	due, _ := e.st.DueFeeds(10)
	if len(due) != 2 {
		t.Fatalf("%d feeds due after the notification, want 2", len(due))
	}
	// Right after a fetch, the next one waits a little.
	e.st.DB.Exec(`UPDATE feeds SET next_fetch_at = ?, last_fetched_at = ?`, time.Now().Unix()+3*3600, time.Now().Unix())
	e.hub.notify(e.hub.key())
	var next int64
	e.st.DB.QueryRow(`SELECT MIN(next_fetch_at) FROM feeds`).Scan(&next)
	if d := next - time.Now().Unix(); d < notifyGap-2 || d > notifyGap {
		t.Fatalf("next fetch in %ds, want about %ds", d, notifyGap)
	}

	// The hub checks only what is pending.
	resp, _ := http.Get(e.m.Callback(w.Token) + "?" + url.Values{"hub.mode": {"subscribe"}, "hub.topic": {feedURL}, "hub.challenge": {"x"}}.Encode())
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected check: HTTP %d", resp.StatusCode)
	}
	resp, _ = http.Get(e.m.Callback(strings.Repeat("0", 32)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown token: HTTP %d", resp.StatusCode)
	}

	// One person removes the feed: still followed. Then the other too.
	e.st.DeleteFeed(e.users[0], e.feeds[0])
	e.m.check(ctx)
	if n := e.hub.count(); n != 1 {
		t.Fatalf("%d requests with a follower left", n)
	}
	e.st.DeleteFeed(e.users[1], e.feeds[1])
	e.m.check(ctx)
	if n := e.hub.count(); n != 2 || e.hub.request(1).Get("hub.mode") != "unsubscribe" ||
		e.hub.request(1).Get("hub.callback") != e.m.Callback(w.Token) {
		t.Fatalf("unsubscribe: %d requests", n)
	}
	if a := e.hub.answer(1); a != "200 OK challenge-unsubscribe" {
		t.Fatalf("callback answered %q", a)
	}
	if _, err := e.st.WebSubByFeedURL(feedURL); err != store.ErrNotFound {
		t.Fatalf("subscription still there: %v", err)
	}
}

func TestRenewAndDenied(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.seen(t)
	e.m.check(ctx)
	// Close to the end of the lease: renewed.
	e.st.DB.Exec(`UPDATE websub SET verified_until = ?`, time.Now().Unix()+600)
	e.m.check(ctx)
	if n := e.hub.count(); n != 2 || e.hub.request(1).Get("hub.mode") != "subscribe" {
		t.Fatalf("renewal: %d requests", n)
	}
	if w := e.sub(t); w.VerifiedUntil < time.Now().Unix()+3500 {
		t.Fatalf("lease not extended: %+v", w)
	}

	// A new PUBLIC_URL: subscribed again with the new callback.
	e.st.DB.Exec(`UPDATE websub SET callback = 'https://old.example/websub/x'`)
	e.m.check(ctx)
	if n := e.hub.count(); n != 3 {
		t.Fatalf("new callback: %d requests", n)
	}

	// The hub refuses: no instant updates, tried again later only.
	e.hub.refuse()
	e.st.DB.Exec(`UPDATE websub SET verified_until = ?`, time.Now().Unix()+600)
	e.m.check(ctx)
	w := e.sub(t)
	if w.VerifiedUntil != 0 || !strings.Contains(w.LastError, "not today") || e.instant(t, 0) {
		t.Fatalf("after denial: %+v", w)
	}
	e.m.check(ctx)
	if n := e.hub.count(); n != 4 {
		t.Fatalf("retried at once: %d requests", n)
	}

	// The feed stops naming a hub: forgotten (the lease is over).
	final, _ := url.Parse(feedURL)
	e.m.Seen(feedURL, nil, []byte(`<rss><channel><title>F</title></channel></rss>`), final)
	e.st.DB.Exec(`UPDATE websub SET last_error = ''`)
	e.m.check(ctx)
	if _, err := e.st.WebSubByFeedURL(feedURL); err != store.ErrNotFound {
		t.Fatalf("subscription without a hub kept: %v", err)
	}
}

func TestHubThatNeverConfirms(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) }))
	defer silent.Close()
	final, _ := url.Parse(feedURL)
	e.m.Seen(feedURL, http.Header{"Link": {"<" + silent.URL + ">; rel=hub"}}, nil, final)
	e.m.check(ctx)
	if w := e.sub(t); w.Pending != store.WebSubSubscribe {
		t.Fatalf("not pending: %+v", w)
	}
	e.m.now = func() time.Time { return time.Now().Add(confirmWait + time.Minute) }
	e.m.check(ctx)
	if w := e.sub(t); w.Pending != "" || w.LastError == "" {
		t.Fatalf("still waiting: %+v", w)
	}
	e.m.now = func() time.Time { return time.Now().Add(retryAfter + time.Minute) }
	e.m.check(ctx)
	if w := e.sub(t); w.Pending != store.WebSubSubscribe {
		t.Fatalf("not tried again: %+v", w)
	}
}

func TestPrivateFeedsAreNotSubscribed(t *testing.T) {
	e := setup(t)
	for i := range e.users {
		if err := e.st.SetFeedAuth(e.users[i], e.feeds[i], "me", "secret"); err != nil {
			t.Fatal(err)
		}
	}
	e.seen(t)
	e.m.check(context.Background())
	if n := e.hub.count(); n != 0 {
		t.Fatalf("%d requests for a feed with a password", n)
	}
}

func TestPublicOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://ebi-furai.example.org":   "https://ebi-furai.example.org",
		"https://Reader.Example.org:8443": "https://reader.example.org:8443",
		"https://8.8.8.8":                 "https://8.8.8.8",
		"https://ebi-furai.example.org/":  "https://ebi-furai.example.org",
		"http://ebi-furai.example.org":    "",
		"https://192.168.1.5:9099":        "",
		"https://localhost":               "",
		"https://nas":                     "",
		"https://nas.local":               "",
		"https://reader.home.arpa":        "",
		"https://reader.lan":              "",
		"null":                            "",
		"":                                "",
	} {
		got, ok := PublicOrigin(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}

// Without PUBLIC_URL the address comes from the administrators' browsers;
// a second one takes over only once the first is no longer used.
func TestLearnedAddress(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	m := New(st, "", nil)
	clock := time.Now()
	m.now = func() time.Time { return clock }
	if m.Base() != "" {
		t.Fatal("an address before any visit")
	}
	m.check(context.Background()) // nothing to do, and no request
	m.Learn("http://192.168.1.5:9099")
	m.Learn("https://nas.local")
	if m.Base() != "" {
		t.Fatalf("learned a local address: %q", m.Base())
	}
	m.Learn("https://reader.example.org")
	if m.Base() != "https://reader.example.org" || m.Callback("t") != "https://reader.example.org/websub/t" {
		t.Fatalf("base %q", m.Base())
	}
	clock = clock.Add(time.Hour)
	m.Learn("https://other.example.org")
	if m.Base() != "https://reader.example.org" {
		t.Fatal("switched while the first address is in use")
	}
	clock = clock.Add(keepLearned + time.Minute)
	m.Learn("https://other.example.org")
	if m.Base() != "https://other.example.org" {
		t.Fatal("did not move to the address in use")
	}
	// Kept across restarts; PUBLIC_URL always wins.
	if New(st, "", nil).Base() != "https://other.example.org" {
		t.Fatal("not kept")
	}
	fixed := New(st, "https://set.example.org/", nil)
	fixed.Learn("https://reader.example.org")
	if fixed.Base() != "https://set.example.org" {
		t.Fatalf("PUBLIC_URL overridden: %q", fixed.Base())
	}
}
