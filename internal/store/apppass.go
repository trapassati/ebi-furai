package store

import (
	"database/sql"
	"errors"
)

// MaxAppPasswords is how many app passwords a user can have.
const MaxAppPasswords = 20

// ErrTooManyAppPasswords is returned past MaxAppPasswords.
var ErrTooManyAppPasswords = errors.New("too many app passwords: remove one you no longer use")

// AppPassword is an app password as listed in the settings (never the
// secret, which is shown only when it is made).
type AppPassword struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	LastIP     string `json:"last_ip"`
}

func (s *Store) AppPasswords(userID int64) ([]AppPassword, error) {
	rows, err := s.DB.Query(`SELECT id, name, created_at, last_used_at, last_ip FROM app_passwords
		WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []AppPassword{}
	for rows.Next() {
		var p AppPassword
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsedAt, &p.LastIP); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// CreateAppPassword stores a new app password (its hash).
func (s *Store) CreateAppPassword(userID int64, name, secretHash string) (*AppPassword, error) {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM app_passwords WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= MaxAppPasswords {
		return nil, ErrTooManyAppPasswords
	}
	p := &AppPassword{Name: truncate(name, 60), CreatedAt: now()}
	res, err := s.DB.Exec(`INSERT INTO app_passwords(user_id, name, secret_hash, created_at) VALUES(?, ?, ?, ?)`,
		userID, p.Name, secretHash, p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.ID, _ = res.LastInsertId()
	return p, nil
}

// DeleteAppPassword revokes an app password and signs out the apps using it.
func (s *Store) DeleteAppPassword(userID, id int64) error {
	return execOne(s.DB, `DELETE FROM app_passwords WHERE id = ? AND user_id = ?`, id, userID)
}

// AppPasswordLogin finds the enabled user an app password belongs to.
func (s *Store) AppPasswordLogin(username, secretHash string) (userID, passwordID int64, err error) {
	err = s.DB.QueryRow(`SELECT u.id, p.id FROM app_passwords p JOIN users u ON u.id = p.user_id
		WHERE p.secret_hash = ? AND u.username = ? AND u.disabled = 0`, secretHash, username).Scan(&userID, &passwordID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// CreateAPIToken stores the token given to an app at sign-in. An app
// signing in again and again does not pile up tokens: only the newest 10
// of each app password are kept.
func (s *Store) CreateAPIToken(tokenHash string, passwordID int64) error {
	if _, err := s.DB.Exec(`INSERT INTO api_tokens(token_hash, app_password_id, created_at) VALUES(?, ?, ?)`,
		tokenHash, passwordID, now()); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM api_tokens WHERE app_password_id = ?1 AND token_hash NOT IN (
		SELECT token_hash FROM api_tokens WHERE app_password_id = ?1 ORDER BY created_at DESC, rowid DESC LIMIT 10)`, passwordID)
	return err
}

// APIUser returns the enabled user of an API token and its app password.
func (s *Store) APIUser(tokenHash string) (*User, int64, error) {
	u := &User{}
	var pid int64
	err := s.DB.QueryRow(`SELECT u.id, u.username, u.is_admin, u.lang, p.id
		FROM api_tokens t JOIN app_passwords p ON p.id = t.app_password_id JOIN users u ON u.id = p.user_id
		WHERE t.token_hash = ? AND u.disabled = 0`, tokenHash).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Lang, &pid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	return u, pid, err
}

// TouchAppPassword records when and from where an app password was used.
func (s *Store) TouchAppPassword(id int64, ip string) error {
	_, err := s.DB.Exec(`UPDATE app_passwords SET last_used_at = ?, last_ip = ? WHERE id = ?`, now(), truncate(ip, 64), id)
	return err
}
