package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

type PushSubscription struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Endpoint  string `json:"-"`
	P256dh    string `json:"-"`
	Auth      string `json:"-"`
	Device    string `json:"device"`
	CreatedAt int64  `json:"created_at"`
	LastOKAt  int64  `json:"last_ok_at"`
	FailCount int    `json:"fail_count"`
	LastError string `json:"last_error"`
}

// SavePushSubscription stores a device subscription. An endpoint belongs to
// one browser: if another user subscribed it before, it moves to this user.
func (s *Store) SavePushSubscription(userID int64, endpoint, p256dh, auth, device string) (int64, error) {
	_, err := s.DB.Exec(`INSERT INTO push_subscriptions(user_id, endpoint, p256dh, auth, device, created_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET user_id = excluded.user_id, p256dh = excluded.p256dh,
			auth = excluded.auth, device = excluded.device, fail_count = 0, last_error = ''`,
		userID, endpoint, p256dh, auth, truncate(device, 80), now())
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.DB.QueryRow(`SELECT id FROM push_subscriptions WHERE endpoint = ?`, endpoint).Scan(&id)
	return id, err
}

func (s *Store) PushSubscriptions(userID int64) ([]*PushSubscription, error) {
	rows, err := s.DB.Query(`SELECT id, user_id, endpoint, p256dh, auth, device, created_at, last_ok_at, fail_count, last_error
		FROM push_subscriptions WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*PushSubscription{}
	for rows.Next() {
		p := &PushSubscription{}
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.Device, &p.CreatedAt, &p.LastOKAt, &p.FailCount, &p.LastError); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) DeletePushSubscription(userID, id int64) error {
	return execOne(s.DB, `DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?`, id, userID)
}

func (s *Store) DeletePushEndpoint(userID int64, endpoint string) error {
	_, err := s.DB.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ? AND user_id = ?`, endpoint, userID)
	return err
}

// DeletePushByID removes a subscription the push service reported as gone.
func (s *Store) DeletePushByID(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM push_subscriptions WHERE id = ?`, id)
	return err
}

func (s *Store) PushResult(id int64, errMsg string) error {
	if errMsg == "" {
		_, err := s.DB.Exec(`UPDATE push_subscriptions SET last_ok_at = ?, fail_count = 0, last_error = '' WHERE id = ?`, now(), id)
		return err
	}
	_, err := s.DB.Exec(`UPDATE push_subscriptions SET fail_count = fail_count + 1, last_error = ? WHERE id = ?`, truncate(errMsg, 200), id)
	return err
}

// ---- digest settings ----

type NotifySettings struct {
	Enabled      bool     `json:"enabled"`
	Times        []string `json:"times"`
	QuietEnabled bool     `json:"quiet_enabled"`
	QuietStart   string   `json:"quiet_start"`
	QuietEnd     string   `json:"quiet_end"`
	LastSlot     string   `json:"-"`
}

func DefaultNotifySettings() *NotifySettings {
	return &NotifySettings{Enabled: true, Times: []string{"08:00", "13:00", "20:00"}, QuietStart: "22:00", QuietEnd: "07:00"}
}

func scanNotify(sc interface{ Scan(...any) error }) (*NotifySettings, error) {
	n := &NotifySettings{}
	var times string
	if err := sc.Scan(&n.Enabled, &times, &n.QuietEnabled, &n.QuietStart, &n.QuietEnd, &n.LastSlot); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(times), &n.Times); err != nil {
		n.Times = []string{}
	}
	return n, nil
}

func (s *Store) NotifySettings(userID int64) (*NotifySettings, error) {
	n, err := scanNotify(s.DB.QueryRow(`SELECT enabled, times, quiet_enabled, quiet_start, quiet_end, last_slot
		FROM notify_settings WHERE user_id = ?`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultNotifySettings(), nil
	}
	return n, err
}

func (s *Store) SaveNotifySettings(userID int64, n *NotifySettings) error {
	times, _ := json.Marshal(n.Times)
	_, err := s.DB.Exec(`INSERT INTO notify_settings(user_id, enabled, times, quiet_enabled, quiet_start, quiet_end)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET enabled = excluded.enabled, times = excluded.times,
			quiet_enabled = excluded.quiet_enabled, quiet_start = excluded.quiet_start, quiet_end = excluded.quiet_end`,
		userID, n.Enabled, string(times), n.QuietEnabled, n.QuietStart, n.QuietEnd)
	return err
}

// DigestUser is a user that has at least one push subscription, with
// the digest schedule.
type DigestUser struct {
	UserID   int64
	Settings *NotifySettings
}

func (s *Store) DigestUsers() ([]DigestUser, error) {
	rows, err := s.DB.Query(`SELECT u.id,
			COALESCE(n.enabled, 1), COALESCE(n.times, '["08:00","13:00","20:00"]'), COALESCE(n.quiet_enabled, 0),
			COALESCE(n.quiet_start, '22:00'), COALESCE(n.quiet_end, '07:00'), COALESCE(n.last_slot, '')
		FROM users u LEFT JOIN notify_settings n ON n.user_id = u.id
		WHERE u.disabled = 0 AND EXISTS (SELECT 1 FROM push_subscriptions p WHERE p.user_id = u.id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DigestUser
	for rows.Next() {
		var id int64
		var times string
		n := &NotifySettings{}
		if err := rows.Scan(&id, &n.Enabled, &times, &n.QuietEnabled, &n.QuietStart, &n.QuietEnd, &n.LastSlot); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(times), &n.Times)
		list = append(list, DigestUser{UserID: id, Settings: n})
	}
	return list, rows.Err()
}

// MarkSlot records that a digest slot has been handled for the user.
func (s *Store) MarkSlot(userID int64, slot string) error {
	_, err := s.DB.Exec(`INSERT INTO notify_settings(user_id, last_slot) VALUES(?, ?)
		ON CONFLICT(user_id) DO UPDATE SET last_slot = excluded.last_slot`, userID, slot)
	return err
}

// FeedUnread is one line of the digest: a feed and its unread count.
type FeedUnread struct {
	Title  string
	Unread int
}

// TopUnreadFeeds returns the feeds with most unread articles.
func (s *Store) TopUnreadFeeds(userID int64, limit int) ([]FeedUnread, error) {
	rows, err := s.DB.Query(`SELECT COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url), COUNT(*) AS n
		FROM articles a JOIN feeds f ON f.id = a.feed_id
		WHERE a.user_id = ? AND a.read = 0 GROUP BY f.id ORDER BY n DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []FeedUnread
	for rows.Next() {
		var f FeedUnread
		if err := rows.Scan(&f.Title, &f.Unread); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

func (s *Store) UnreadCount(userID int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM articles WHERE user_id = ? AND read = 0`, userID).Scan(&n)
	return n, err
}
