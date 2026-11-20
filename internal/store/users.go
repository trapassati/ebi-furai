package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrUserExists = errors.New("username already taken")

type User struct {
	ID        int64           `json:"id"`
	Username  string          `json:"username"`
	IsAdmin   bool            `json:"is_admin"`
	Disabled  bool            `json:"disabled"`
	Prefs     json.RawMessage `json:"prefs,omitempty"`
	CreatedAt int64           `json:"created_at"`
	FeedCount int             `json:"feed_count,omitempty"`
	// RefreshMinutes: feed refresh interval chosen by the user (0 = server default).
	RefreshMinutes int `json:"refresh_minutes"`
	// Dedup: the same article from two feeds counts once.
	Dedup bool `json:"dedup"`
	// Lang: the language of the app last used, for notifications.
	Lang string `json:"lang"`
}

const userCols = `id, username, is_admin, disabled, prefs, created_at, refresh_minutes, dedup`

func scanUser(sc interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var prefs string
	if err := sc.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Disabled, &prefs, &u.CreatedAt, &u.RefreshMinutes, &u.Dedup); err != nil {
		return nil, err
	}
	u.Prefs = json.RawMessage(prefs)
	return u, nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, hash string, admin bool) (*User, error) {
	res, err := s.DB.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES(?, ?, ?, ?)`,
		username, hash, admin, now())
	if isUnique(err) {
		return nil, ErrUserExists
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(id)
}

func (s *Store) UserByID(id int64) (*User, error) {
	u, err := scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// UserForLogin returns the user and its password hash.
func (s *Store) UserForLogin(username string) (*User, string, error) {
	var hash string
	u := &User{}
	var prefs string
	err := s.DB.QueryRow(`SELECT `+userCols+`, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Disabled, &prefs, &u.CreatedAt, &u.RefreshMinutes, &u.Dedup, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	u.Prefs = json.RawMessage(prefs)
	return u, hash, err
}

func (s *Store) PasswordHash(userID int64) (string, error) {
	var h string
	err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.DB.Query(`SELECT u.id, u.username, u.is_admin, u.disabled, '{}', u.created_at,
		(SELECT COUNT(*) FROM feeds f WHERE f.user_id = u.id)
		FROM users u ORDER BY u.username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*User{}
	for rows.Next() {
		u := &User{}
		var prefs string
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Disabled, &prefs, &u.CreatedAt, &u.FeedCount); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

func (s *Store) SetPassword(userID int64, hash string) error {
	return execOne(s.DB, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
}

func (s *Store) SetDisabled(userID int64, disabled bool) error {
	if err := execOne(s.DB, `UPDATE users SET disabled = ? WHERE id = ?`, disabled, userID); err != nil {
		return err
	}
	if disabled {
		return s.DeleteUserSessions(userID, "")
	}
	return nil
}

func (s *Store) DeleteUser(userID int64) error {
	// Articles reference feeds (cascade), feeds/folders/sessions reference users.
	return execOne(s.DB, `DELETE FROM users WHERE id = ?`, userID)
}

func (s *Store) SetPrefs(userID int64, prefs json.RawMessage) error {
	return execOne(s.DB, `UPDATE users SET prefs = ? WHERE id = ?`, string(prefs), userID)
}

func execOne(db execer, q string, args ...any) error {
	res, err := db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- sessions ----

type Session struct {
	User     *User
	CSRF     string
	LastSeen int64
}

func (s *Store) CreateSession(tokenHash string, userID int64, csrf, ua, ip string, ttl time.Duration) error {
	t := now()
	_, err := s.DB.Exec(`INSERT INTO sessions(token_hash, user_id, csrf_token, created_at, last_seen, expires_at, user_agent, ip)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, tokenHash, userID, csrf, t, t, t+int64(ttl.Seconds()), truncate(ua, 200), truncate(ip, 64))
	return err
}

// SessionByToken returns the session for a valid, unexpired token whose
// user is enabled.
func (s *Store) SessionByToken(tokenHash string) (*Session, error) {
	row := s.DB.QueryRow(`SELECT u.id, u.username, u.is_admin, u.disabled, u.prefs, u.created_at, u.refresh_minutes, u.dedup, u.lang, s.csrf_token, s.last_seen
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled = 0`, tokenHash, now())
	u := &User{}
	sess := &Session{User: u}
	var prefs string
	err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Disabled, &prefs, &u.CreatedAt, &u.RefreshMinutes, &u.Dedup, &u.Lang, &sess.CSRF, &sess.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Prefs = json.RawMessage(prefs)
	return sess, nil
}

func (s *Store) TouchSession(tokenHash, ip string, ttl time.Duration) error {
	t := now()
	_, err := s.DB.Exec(`UPDATE sessions SET last_seen = ?, expires_at = ?, ip = ? WHERE token_hash = ?`,
		t, t+int64(ttl.Seconds()), truncate(ip, 64), tokenHash)
	return err
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteUserSessions removes all sessions of a user except keepHash.
func (s *Store) DeleteUserSessions(userID int64, keepHash string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepHash)
	return err
}

func (s *Store) PurgeExpiredSessions() error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now())
	return err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// RefreshChoices are the intervals a user can pick, in minutes (0 = default).
var RefreshChoices = []int{0, 5, 10, 15, 20, 30, 60, 120, 360, 720, 1440}

func (s *Store) SetDedup(userID int64, on bool) error {
	return execOne(s.DB, `UPDATE users SET dedup = ? WHERE id = ?`, on, userID)
}

// SetRefreshMinutes stores the user's interval and moves the next refresh
// of their healthy feeds so that a shorter interval applies right away.
func (s *Store) SetRefreshMinutes(userID int64, minutes int, effective time.Duration) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET refresh_minutes = ? WHERE id = ?`, minutes, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE feeds SET next_fetch_at = MIN(next_fetch_at, last_fetched_at + ?)
		WHERE user_id = ? AND error_count = 0`, int64(effective.Seconds()), userID); err != nil {
		return err
	}
	return tx.Commit()
}

// UserLang returns the language of the user's notifications.
func (s *Store) UserLang(userID int64) string {
	var lang string
	if s.DB.QueryRow(`SELECT lang FROM users WHERE id = ?`, userID).Scan(&lang) != nil {
		return "en"
	}
	return lang
}

// SetUserLang remembers the language the user's app is in.
func (s *Store) SetUserLang(userID int64, lang string) error {
	_, err := s.DB.Exec(`UPDATE users SET lang = ? WHERE id = ? AND lang <> ?`, lang, userID, lang)
	return err
}
