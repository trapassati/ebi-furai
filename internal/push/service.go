package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/i18n"
	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
)

const (
	settingVAPIDKey  = "vapid_private_key"
	settingPublicURL = "public_url"
	catchUp          = 30 * time.Minute // a slot missed (e.g. restart) is still sent within this window
	maxFailures      = 20               // subscriptions failing this many times in a row are dropped
)

// Service sends digest notifications.
type Service struct {
	store  *store.Store
	cfg    *config.Config
	vapid  *VAPID
	client *http.Client
}

// NewService loads the VAPID keys, generating them on first start.
func NewService(st *store.Store, cfg *config.Config) (*Service, error) {
	var v *VAPID
	priv, err := st.Setting(settingVAPIDKey)
	switch {
	case err == nil:
		v, err = LoadVAPID(priv)
	case errors.Is(err, store.ErrNotFound):
		v, priv, err = GenerateVAPID()
		if err == nil {
			err = st.SetSetting(settingVAPIDKey, priv)
			slog.Info("VAPID keys generated")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("VAPID: %w", err)
	}
	return &Service{store: st, cfg: cfg, vapid: v, client: netsafe.NewClient(30 * time.Second)}, nil
}

func (s *Service) PublicKey() string { return s.vapid.PublicKey }

// RememberOrigin stores the public https address of the app, used as the
// VAPID contact ("sub") unless VAPID_SUBJECT is set.
func (s *Service) RememberOrigin(origin string) {
	if !strings.HasPrefix(origin, "https://") {
		return
	}
	if cur, _ := s.store.Setting(settingPublicURL); cur != origin {
		s.store.SetSetting(settingPublicURL, origin)
	}
}

func (s *Service) subject() string {
	if s.cfg.VAPIDSubject != "" {
		return s.cfg.VAPIDSubject
	}
	if u, err := s.store.Setting(settingPublicURL); err == nil && u != "" {
		return u
	}
	return "mailto:admin@example.com"
}

// payload is what the service worker receives.
type payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Badge int    `json:"badge"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// digest builds the summary for a user; ok is false when nothing is unread.
func (s *Service) digest(userID int64) (payload, bool, error) {
	n, err := s.store.UnreadCount(userID)
	if err != nil || n == 0 {
		return payload{}, false, err
	}
	lang := s.store.UserLang(userID)
	body := i18n.T(lang, fmt.Sprintf("%d articles to read", n))
	if n == 1 {
		body = i18n.T(lang, "1 article to read")
	}
	top, err := s.store.TopUnreadFeeds(userID, 4)
	if err != nil {
		return payload{}, false, err
	}
	if len(top) > 0 {
		parts := []string{}
		for i, f := range top {
			if i == 3 {
				parts = append(parts, i18n.T(lang, "and more"))
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%d)", f.Title, f.Unread))
		}
		body += "\n" + strings.Join(parts, ", ")
	}
	return payload{Title: s.cfg.AppName, Body: body, Badge: n, URL: "/#/list/unread", Tag: "digest"}, true, nil
}

// SendResult summarizes a delivery to all devices of a user.
type SendResult struct {
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
	Unread int `json:"unread"`
}

// SendDigest sends the current digest to every device of the user. With
// force, a notification is sent even when nothing is unread (test).
func (s *Service) SendDigest(ctx context.Context, userID int64, force bool) (SendResult, error) {
	p, ok, err := s.digest(userID)
	if err != nil {
		return SendResult{}, err
	}
	if !ok {
		if !force {
			return SendResult{}, nil
		}
		p = payload{Title: s.cfg.AppName, Body: i18n.T(s.store.UserLang(userID), "You've read everything, no articles waiting"), Badge: 0, URL: "/", Tag: "digest"}
	}
	res := SendResult{Unread: p.Badge}
	body, _ := json.Marshal(p)
	subs, err := s.store.PushSubscriptions(userID)
	if err != nil {
		return res, err
	}
	msg := Message{Payload: body, TTL: 6 * time.Hour, Topic: "digest", Urgency: "normal"}
	for _, sub := range subs {
		err := s.vapid.Send(ctx, s.client, Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}, s.subject(), msg)
		var se *SendError
		switch {
		case err == nil:
			res.Sent++
			s.store.PushResult(sub.ID, "")
		case errors.As(err, &se) && se.Gone():
			res.Failed++
			s.store.DeletePushByID(sub.ID)
			slog.Info("push subscription gone, removed", "user", userID, "device", sub.Device)
		default:
			res.Failed++
			s.store.PushResult(sub.ID, friendlyError(err))
			if sub.FailCount+1 >= maxFailures {
				s.store.DeletePushByID(sub.ID)
			}
			slog.Warn("push failed", "user", userID, "device", sub.Device, "err", err)
		}
	}
	return res, nil
}

// Run checks the digest schedules every minute until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.tick(ctx, time.Now().In(s.cfg.Location))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) tick(ctx context.Context, now time.Time) {
	users, err := s.store.DigestUsers()
	if err != nil {
		slog.Error("digest users", "err", err)
		return
	}
	for _, u := range users {
		if !u.Settings.Enabled {
			continue
		}
		key, hhmm := DueSlot(u.Settings.Times, u.Settings.LastSlot, now)
		if key == "" {
			continue
		}
		// Mark first: a slot is handled once, even if it is skipped.
		if err := s.store.MarkSlot(u.UserID, key); err != nil {
			slog.Error("mark slot", "err", err)
			continue
		}
		if u.Settings.QuietEnabled && InQuiet(hhmm, u.Settings.QuietStart, u.Settings.QuietEnd) {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res, err := s.SendDigest(sctx, u.UserID, false)
		cancel()
		if err != nil {
			slog.Error("digest", "user", u.UserID, "err", err)
		} else if res.Sent+res.Failed > 0 {
			slog.Info("digest sent", "user", u.UserID, "slot", hhmm, "unread", res.Unread, "sent", res.Sent, "failed", res.Failed)
		}
	}
}

// ---- schedule helpers ----

// ParseHHMM returns the minutes after midnight of "HH:MM".
func ParseHHMM(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// DueSlot returns the latest slot of today that is due (its time passed
// less than catchUp ago) and differs from lastSlot. key is "YYYY-MM-DD HH:MM".
func DueSlot(times []string, lastSlot string, now time.Time) (key, hhmm string) {
	cur := now.Hour()*60 + now.Minute()
	sorted := slices.Clone(times)
	slices.Sort(sorted)
	for _, t := range slices.Backward(sorted) {
		m, ok := ParseHHMM(t)
		if !ok || m > cur || time.Duration(cur-m)*time.Minute >= catchUp {
			continue
		}
		k := now.Format("2006-01-02") + " " + t
		if k == lastSlot {
			return "", ""
		}
		return k, t
	}
	return "", ""
}

// InQuiet reports whether hhmm falls in the quiet window [start, end),
// which may cross midnight.
func InQuiet(hhmm, start, end string) bool {
	t, ok1 := ParseHHMM(hhmm)
	s, ok2 := ParseHHMM(start)
	e, ok3 := ParseHHMM(end)
	if !ok1 || !ok2 || !ok3 || s == e {
		return false
	}
	if s < e {
		return t >= s && t < e
	}
	return t >= s || t < e
}

// friendlyError is the message shown in the device list.
func friendlyError(err error) string {
	var se *SendError
	if errors.As(err, &se) {
		switch se.Status {
		case http.StatusForbidden, http.StatusUnauthorized:
			return fmt.Sprintf("the push service refused the message (HTTP %d %s)", se.Status, se.Body)
		case http.StatusTooManyRequests:
			return "too many notifications, try again later"
		}
		return fmt.Sprintf("the push service answered HTTP %d %s", se.Status, se.Body)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the push service did not respond in time"
	}
	return "push service unreachable"
}
