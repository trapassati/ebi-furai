package websub

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
)

const (
	leaseAsked     = 10 * 24 * time.Hour  // asked of the hub, which decides
	maxLease       = 365 * 24 * time.Hour // a longer lease from a hub is cut to this
	renewBefore    = 24 * time.Hour       // renewal this long before the lease ends (at most a quarter of it)
	confirmWait    = time.Hour            // a hub that has not checked the callback by then has failed
	retryAfter     = 6 * time.Hour        // after a refusal or an error
	checkEvery     = 5 * time.Minute
	requestTimeout = 20 * time.Second
	notifyGap      = 15 // seconds between two fetches caused by notifications
	maxNotifyBody  = 10 << 20
	maxChallenge   = 1024
	// keepLearned: a learned address gives way to another one only after
	// it has not been used for this long (two addresses in use do not make
	// the subscriptions move back and forth).
	keepLearned    = 24 * time.Hour
	settingLearned = "websub_public_url"
)

// Manager subscribes feeds to their hubs and receives the hubs' calls.
type Manager struct {
	store  *store.Store
	fixed  string // PUBLIC_URL, without a final "/"; empty = learned
	client *http.Client
	wake   func() // asks the fetcher to look for due feeds
	kick   chan struct{}
	now    func() time.Time
	// checkURL accepts a hub address (public http(s) only; tests use
	// local hubs).
	checkURL func(string) (*url.URL, error)

	mu          sync.Mutex
	learned     string    // the public address seen in the administrators' browsers
	learnedSeen time.Time // when it was last used
}

// New returns a manager for a server reachable at publicURL, or, when it is
// empty, at the address the administrators open the app from (see Learn);
// wake is called when a notification makes feeds due.
func New(st *store.Store, publicURL string, wake func()) *Manager {
	m := &Manager{
		store:    st,
		fixed:    strings.TrimRight(publicURL, "/"),
		client:   netsafe.NewClient(requestTimeout),
		wake:     wake,
		kick:     make(chan struct{}, 1),
		now:      time.Now,
		checkURL: netsafe.ValidateURL,
	}
	if m.fixed == "" {
		m.learned, _ = st.Setting(settingLearned)
		m.learnedSeen = time.Now()
	}
	return m
}

// Base is the public address of the server, without a final "/": empty
// while it is not known (then nothing is subscribed).
func (m *Manager) Base() string {
	if m.fixed != "" {
		return m.fixed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.learned
}

// Callback is the address a hub calls for a subscription.
func (m *Manager) Callback(token string) string { return m.Base() + "/websub/" + token }

// Learn notes the address an administrator's browser opened the app from
// (its Origin header), when it is an https address on the internet: that
// is where the hubs can call. Nothing to set by hand, and a new domain is
// picked up by itself (the subscriptions then move to it).
func (m *Manager) Learn(origin string) {
	if m.fixed != "" {
		return
	}
	base, ok := PublicOrigin(origin)
	if !ok {
		return
	}
	m.mu.Lock()
	now := m.now()
	if base == m.learned {
		m.learnedSeen = now
		m.mu.Unlock()
		return
	}
	if m.learned != "" && now.Sub(m.learnedSeen) < keepLearned {
		m.mu.Unlock()
		return
	}
	m.learned, m.learnedSeen = base, now
	m.mu.Unlock()
	if err := m.store.SetSetting(settingLearned, base); err != nil {
		slog.Error("websub: saving the public address", "err", err)
	}
	slog.Info("websub: public address learned from the app", "url", base)
	m.Kick()
}

// PublicOrigin returns origin ("https://host[:port]") when hubs on the
// internet can plausibly reach it: https, and a public IP or a domain name
// that is not for local networks only.
func PublicOrigin(origin string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if !netsafe.IsPublicIP(ip) {
			return "", false
		}
	} else {
		if !strings.Contains(host, ".") {
			return "", false // localhost, a machine name
		}
		for _, local := range []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".intranet", ".corp",
			".localhost", ".localdomain", ".test", ".example", ".invalid"} {
			if strings.HasSuffix(host, local) {
				return "", false
			}
		}
	}
	return "https://" + strings.ToLower(u.Host), true
}

// Fixed reports whether the public address comes from PUBLIC_URL (rather
// than being learned from the app).
func (m *Manager) Fixed() bool { return m.fixed != "" }

// RetryFailed asks the hubs again, now, for the subscriptions that went
// wrong (the administrator's "try again").
func (m *Manager) RetryFailed() error {
	if err := m.store.WebSubRetryFailed(); err != nil {
		return err
	}
	m.Kick()
	return nil
}

func (m *Manager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Seen is called after each successful download of a feed (that needs no
// password) with its answer: it notes the hub the feed names, if any.
func (m *Manager) Seen(feedURL string, h http.Header, body []byte, final *url.URL) {
	hub, self := Discover(h, body, final)
	if hub != "" {
		if _, err := m.checkURL(hub); err != nil {
			hub = ""
		}
	}
	topic := self
	if topic == "" && final != nil {
		topic = final.String()
	}
	changed, err := m.store.WebSubSeen(feedURL, hub, topic)
	if err != nil {
		slog.Error("websub: saving the hub", "feed", feedURL, "err", err)
		return
	}
	if changed {
		m.Kick()
	}
}

// Run keeps the subscriptions in order until ctx ends: new ones, renewals
// before the lease ends, and goodbyes when nobody follows a feed any more.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(checkEvery)
	defer t.Stop()
	for {
		m.check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.kick:
		}
	}
}

func (m *Manager) check(ctx context.Context) {
	if m.Base() == "" {
		return // not known yet: an administrator has not opened the app over https
	}
	list, err := m.store.WebSubs()
	if err != nil {
		slog.Error("websub: reading the subscriptions", "err", err)
		return
	}
	for _, w := range list {
		if ctx.Err() != nil {
			return
		}
		m.checkOne(ctx, w)
	}
}

func (m *Manager) checkOne(ctx context.Context, w *store.WebSub) {
	now := m.now()
	since := now.Sub(time.Unix(w.RequestedAt, 0))
	verified := w.VerifiedUntil > now.Unix()
	if w.Pending != "" {
		if since < confirmWait {
			return // waiting for the hub
		}
		if w.Pending == store.WebSubUnsubscribe {
			m.store.DeleteWebSub(w.ID)
			return
		}
		m.store.WebSubFailed(w.ID, "the hub did not confirm the subscription")
		return // tried again after retryAfter
	}
	if w.LastError != "" && since < retryAfter {
		return
	}
	if !w.Followed || !w.Declared {
		if !verified {
			// The hub stops by itself when the lease ends.
			m.store.DeleteWebSub(w.ID)
			return
		}
		m.request(ctx, w, store.WebSubUnsubscribe)
		return
	}
	if verified && w.Callback == m.Callback(w.Token) && !renewDue(w, now) {
		return
	}
	m.request(ctx, w, store.WebSubSubscribe)
}

func renewDue(w *store.WebSub, now time.Time) bool {
	margin := min(time.Duration(w.LeaseSeconds)*time.Second/4, renewBefore)
	return now.Add(margin).Unix() >= w.VerifiedUntil
}

// request asks the hub to subscribe or unsubscribe. The hub answers 202
// and then checks the callback (sometimes before answering).
func (m *Manager) request(ctx context.Context, w *store.WebSub, mode string) {
	cb := m.Callback(w.Token)
	if mode == store.WebSubUnsubscribe && w.Callback != "" {
		cb = w.Callback // the one the hub knows
	}
	if err := m.store.WebSubRequesting(w.ID, mode, cb); err != nil {
		slog.Error("websub: saving the request", "err", err)
		return
	}
	form := url.Values{
		"hub.mode":     {mode},
		"hub.topic":    {w.Topic},
		"hub.callback": {cb},
	}
	if mode == store.WebSubSubscribe {
		form.Set("hub.secret", w.Secret)
		form.Set("hub.lease_seconds", strconv.Itoa(int(leaseAsked/time.Second)))
	}
	if err := m.post(ctx, w.Hub, form); err != nil {
		if ctx.Err() == nil {
			m.store.WebSubFailed(w.ID, err.Error())
			slog.Info("websub: request failed", "mode", mode, "hub", w.Hub, "topic", w.Topic, "err", err)
		} else {
			m.store.WebSubFailed(w.ID, "")
		}
		return
	}
	slog.Info("websub: request sent", "mode", mode, "hub", w.Hub, "topic", w.Topic)
}

func (m *Manager) post(ctx context.Context, hub string, form url.Values) error {
	u, err := m.checkURL(hub)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", netsafe.UserAgent)
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the hub answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// Handle answers the hub at /websub/{token}: GET to confirm (or refuse) a
// subscription, POST to say the feed changed. No sign-in: the token in the
// address finds the subscription, and notifications must be signed with
// its secret.
func (m *Manager) Handle(w http.ResponseWriter, r *http.Request, token string) {
	if len(token) != 32 {
		http.NotFound(w, r)
		return
	}
	sub, err := m.store.WebSubByToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("websub: reading the subscription", "err", err)
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		m.verify(w, r, sub)
	case http.MethodPost:
		m.notify(w, r, sub)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) verify(w http.ResponseWriter, r *http.Request, sub *store.WebSub) {
	q := r.URL.Query()
	mode, challenge := q.Get("hub.mode"), q.Get("hub.challenge")
	if q.Get("hub.topic") != sub.Topic {
		http.NotFound(w, r)
		return
	}
	switch mode {
	case "denied":
		m.store.WebSubDenied(sub.ID, q.Get("hub.reason"))
		slog.Info("websub: the hub refused the subscription", "topic", sub.Topic, "reason", q.Get("hub.reason"))
		w.WriteHeader(http.StatusOK)
		return
	case store.WebSubSubscribe, store.WebSubUnsubscribe:
	default:
		http.NotFound(w, r)
		return
	}
	if sub.Pending != mode || challenge == "" || len(challenge) > maxChallenge {
		http.NotFound(w, r)
		return
	}
	if mode == store.WebSubUnsubscribe {
		if err := m.store.DeleteWebSub(sub.ID); err != nil {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		slog.Info("websub: unsubscribed", "topic", sub.Topic)
	} else {
		lease, err := strconv.ParseInt(q.Get("hub.lease_seconds"), 10, 64)
		if err != nil || lease <= 0 {
			lease = int64(leaseAsked / time.Second)
		}
		lease = min(lease, int64(maxLease/time.Second))
		if err := m.store.WebSubVerified(sub.ID, lease); err != nil {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		slog.Info("websub: subscribed", "topic", sub.Topic, "lease", time.Duration(lease)*time.Second)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, challenge)
}

func (m *Manager) notify(w http.ResponseWriter, r *http.Request, sub *store.WebSub) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxNotifyBody))
	if err != nil {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	// The specification wants a 2xx answer even when the signature is
	// wrong (the message is just ignored), so the sender learns nothing.
	if !ValidSignature(r.Header.Get("X-Hub-Signature"), sub.Secret, body) {
		slog.Warn("websub: notification with a missing or wrong signature ignored", "topic", sub.Topic)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if sub.Followed && sub.Declared {
		n, err := m.store.WebSubNotified(sub.ID, sub.FeedURL, notifyGap)
		if err != nil {
			slog.Error("websub: scheduling the feed", "err", err)
		} else if n > 0 && m.wake != nil {
			m.wake()
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// ValidSignature checks an X-Hub-Signature header ("sha256=<hex>", or
// sha1, sha384, sha512): the HMAC of the body with the secret.
func ValidSignature(header, secret string, body []byte) bool {
	method, sig, ok := strings.Cut(strings.TrimSpace(header), "=")
	if !ok || secret == "" {
		return false
	}
	var h func() hash.Hash
	switch strings.ToLower(method) {
	case "sha1":
		h = sha1.New
	case "sha256":
		h = sha256.New
	case "sha384":
		h = sha512.New384
	case "sha512":
		h = sha512.New
	default:
		return false
	}
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(h, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}
