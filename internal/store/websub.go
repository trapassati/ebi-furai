package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
)

// WebSub is the subscription of a feed address to its hub.
type WebSub struct {
	ID            int64
	FeedURL       string
	Hub           string
	Topic         string
	Declared      bool
	Token         string
	Secret        string
	Callback      string
	Pending       string // "subscribe", "unsubscribe" or empty
	LeaseSeconds  int64
	VerifiedUntil int64
	RequestedAt   int64
	NotifiedAt    int64
	LastError     string
	// Followed: at least one active user follows the address, without a
	// password (private feeds are never announced to a hub).
	Followed bool
}

const (
	WebSubSubscribe   = "subscribe"
	WebSubUnsubscribe = "unsubscribe"
)

const websubCols = `id, feed_url, hub, topic, declared, token, secret, callback, pending, lease_seconds, verified_until,
	requested_at, notified_at, last_error,
	EXISTS (SELECT 1 FROM feeds f JOIN users u ON u.id = f.user_id
		WHERE f.url = websub.feed_url AND f.kind = 'feed' AND f.auth_user = '' AND u.disabled = 0)`

func scanWebSub(sc interface{ Scan(...any) error }) (*WebSub, error) {
	w := &WebSub{}
	err := sc.Scan(&w.ID, &w.FeedURL, &w.Hub, &w.Topic, &w.Declared, &w.Token, &w.Secret, &w.Callback, &w.Pending,
		&w.LeaseSeconds, &w.VerifiedUntil, &w.RequestedAt, &w.NotifiedAt, &w.LastError, &w.Followed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// WebSubSeen records what a successful fetch of a feed said about its hub.
// It reports whether the subscription is new or its hub or topic changed
// (a request to the hub is then needed). An empty hub means the feed names
// none (any more).
func (s *Store) WebSubSeen(feedURL, hub, topic string) (bool, error) {
	// Most fetches change nothing: a read first spares a write each.
	var oldHub, oldTopic string
	var declared bool
	err := s.DB.QueryRow(`SELECT hub, topic, declared FROM websub WHERE feed_url = ?`, feedURL).Scan(&oldHub, &oldTopic, &declared)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if hub == "" {
			return false, nil
		}
	case err != nil:
		return false, err
	case hub == "" && !declared, hub != "" && declared && oldHub == hub && oldTopic == topic:
		return false, nil
	}
	if hub == "" {
		_, err := s.DB.Exec(`UPDATE websub SET declared = 0 WHERE feed_url = ? AND declared = 1`, feedURL)
		return false, err
	}
	res, err := s.DB.Exec(`INSERT INTO websub(feed_url, hub, topic, token, secret, created_at) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url) DO UPDATE SET hub = excluded.hub, topic = excluded.topic, declared = 1, last_error = '',
			requested_at = 0, verified_until = 0, pending = ''
		WHERE websub.hub <> excluded.hub OR websub.topic <> excluded.topic OR websub.declared = 0`,
		feedURL, hub, topic, randomHex(16), randomHex(32), now())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// WebSubs returns every subscription (for the periodic check).
func (s *Store) WebSubs() ([]*WebSub, error) {
	rows, err := s.DB.Query(`SELECT ` + websubCols + ` FROM websub ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*WebSub
	for rows.Next() {
		w, err := scanWebSub(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, rows.Err()
}

func (s *Store) WebSubByToken(token string) (*WebSub, error) {
	return scanWebSub(s.DB.QueryRow(`SELECT `+websubCols+` FROM websub WHERE token = ?`, token))
}

func (s *Store) WebSubByFeedURL(feedURL string) (*WebSub, error) {
	return scanWebSub(s.DB.QueryRow(`SELECT `+websubCols+` FROM websub WHERE feed_url = ?`, feedURL))
}

// WebSubRequesting marks a request about to be sent to the hub. It is
// stored first: some hubs check the callback before they even answer.
func (s *Store) WebSubRequesting(id int64, mode, callback string) error {
	_, err := s.DB.Exec(`UPDATE websub SET pending = ?, callback = ?, requested_at = ?, last_error = '' WHERE id = ?`,
		mode, callback, now(), id)
	return err
}

// WebSubFailed records a request the hub refused or never confirmed.
func (s *Store) WebSubFailed(id int64, msg string) error {
	_, err := s.DB.Exec(`UPDATE websub SET pending = '', last_error = ? WHERE id = ?`, truncate(msg, 300), id)
	return err
}

// WebSubVerified: the hub confirmed the subscription for lease seconds.
func (s *Store) WebSubVerified(id, lease int64) error {
	_, err := s.DB.Exec(`UPDATE websub SET pending = '', lease_seconds = ?, verified_until = ?, last_error = '' WHERE id = ?`,
		lease, now()+lease, id)
	return err
}

// WebSubDenied: the hub refused or ended the subscription.
func (s *Store) WebSubDenied(id int64, reason string) error {
	_, err := s.DB.Exec(`UPDATE websub SET pending = '', verified_until = 0, last_error = ?, requested_at = ? WHERE id = ?`,
		truncate("denied by the hub: "+reason, 300), now(), id)
	return err
}

func (s *Store) DeleteWebSub(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM websub WHERE id = ?`, id)
	return err
}

// WebSubNotified records a valid notification and has every feed with that
// address fetched as soon as possible, but not more than once every
// minGap seconds: a hub that calls in bursts does not cause a fetch each.
func (s *Store) WebSubNotified(id int64, feedURL string, minGap int64) (int64, error) {
	if _, err := s.DB.Exec(`UPDATE websub SET notified_at = ? WHERE id = ?`, now(), id); err != nil {
		return 0, err
	}
	res, err := s.DB.Exec(`UPDATE feeds SET next_fetch_at = MIN(next_fetch_at, MAX(?, last_fetched_at + ?))
		WHERE url = ? AND kind = 'feed'`, now(), minGap, feedURL)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// WebSubHub sums up the subscriptions to one hub.
type WebSubHub struct {
	Host    string `json:"host"`
	Active  int    `json:"active"`
	Failing int    `json:"failing"`
}

// WebSubProblem is a subscription the hub refused or never confirmed.
type WebSubProblem struct {
	Title string `json:"title"` // empty when the administrator does not follow the feed
	Hub   string `json:"hub"`
	Error string `json:"error"`
	At    int64  `json:"at"`
}

// WebSubReport is what the administrators see of the instant updates.
type WebSubReport struct {
	Followed     int             `json:"followed"` // feed addresses followed without a password
	WithHub      int             `json:"with_hub"` // of those, how many name a hub
	Active       int             `json:"active"`
	Pending      int             `json:"pending"`
	Failing      int             `json:"failing"`
	LastNotified int64           `json:"last_notified"`
	Hubs         []WebSubHub     `json:"hubs"`
	Problems     []WebSubProblem `json:"problems"`
}

// WebSubReport sums up the subscriptions for the administration page.
// Only the feeds adminID follows are named: the others' subscriptions are
// their own business (they show as "a feed of another user").
func (s *Store) WebSubReport(adminID int64) (*WebSubReport, error) {
	r := &WebSubReport{Hubs: []WebSubHub{}, Problems: []WebSubProblem{}}
	if err := s.DB.QueryRow(`SELECT COUNT(DISTINCT f.url) FROM feeds f JOIN users u ON u.id = f.user_id
		WHERE f.kind = 'feed' AND f.auth_user = '' AND u.disabled = 0`).Scan(&r.Followed); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(notified_at), 0) FROM websub`).Scan(&r.LastNotified); err != nil {
		return nil, err
	}
	list, err := s.WebSubs()
	if err != nil {
		return nil, err
	}
	t := now()
	hubs := map[string]*WebSubHub{}
	var order []string
	for _, w := range list {
		if !w.Followed || !w.Declared {
			continue // on its way out
		}
		r.WithHub++
		host := w.Hub
		if u, err := url.Parse(w.Hub); err == nil && u.Host != "" {
			host = u.Host
		}
		hub := hubs[host]
		if hub == nil {
			hub = &WebSubHub{Host: host}
			hubs[host] = hub
			order = append(order, host)
		}
		switch {
		case w.VerifiedUntil > t:
			r.Active++
			hub.Active++
		case w.LastError != "":
			r.Failing++
			hub.Failing++
			p := WebSubProblem{Hub: host, Error: w.LastError, At: w.RequestedAt}
			err := s.DB.QueryRow(`SELECT title FROM feeds WHERE user_id = ? AND url = ? AND kind = 'feed' LIMIT 1`,
				adminID, w.FeedURL).Scan(&p.Title)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			r.Problems = append(r.Problems, p)
		default:
			r.Pending++
		}
	}
	for _, h := range order {
		r.Hubs = append(r.Hubs, *hubs[h])
	}
	sort.SliceStable(r.Hubs, func(i, j int) bool {
		a, b := r.Hubs[i], r.Hubs[j]
		if a.Active+a.Failing != b.Active+b.Failing {
			return a.Active+a.Failing > b.Active+b.Failing
		}
		return a.Host < b.Host
	})
	sort.SliceStable(r.Problems, func(i, j int) bool { return r.Problems[i].At > r.Problems[j].At })
	return r, nil
}

// WebSubRetryFailed has the subscriptions that went wrong asked again at
// the next check, without waiting for the usual pause.
func (s *Store) WebSubRetryFailed() error {
	_, err := s.DB.Exec(`UPDATE websub SET requested_at = 0 WHERE last_error <> '' AND pending = ''`)
	return err
}
