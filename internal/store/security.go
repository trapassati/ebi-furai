package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
)

// ---- two-step verification (TOTP) ----
// The secret is sealed like the feed passwords, bound to the user id.

func totpAAD(userID int64) []byte {
	return []byte("totp:" + strconv.FormatInt(userID, 10))
}

func (s *Store) sealTOTP(userID int64, secret []byte) ([]byte, error) {
	aead, err := s.feedAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, secret, totpAAD(userID)), nil
}

func (s *Store) openTOTP(userID int64, sealed []byte) ([]byte, error) {
	aead, err := s.feedAEAD()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("damaged secret")
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], totpAAD(userID))
}

// TOTPState is the user's two-step verification: secret (nil if none),
// whether it is active, and how many recovery codes are left.
type TOTPState struct {
	Secret        []byte
	Enabled       bool
	LastStep      int64
	RecoveryCodes int
}

func (s *Store) TOTP(userID int64) (*TOTPState, error) {
	st := &TOTPState{}
	var sealed []byte
	err := s.DB.QueryRow(`SELECT totp_secret, totp_enabled, totp_last_step,
		(SELECT COUNT(*) FROM recovery_codes WHERE user_id = users.id) FROM users WHERE id = ?`, userID).
		Scan(&sealed, &st.Enabled, &st.LastStep, &st.RecoveryCodes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(sealed) > 0 {
		if st.Secret, err = s.openTOTP(userID, sealed); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// SetTOTPPending stores a new secret, not active until confirmed with a
// code (EnableTOTP). Refused while one is active.
func (s *Store) SetTOTPPending(userID int64, secret []byte) error {
	sealed, err := s.sealTOTP(userID, secret)
	if err != nil {
		return err
	}
	return execOne(s.DB, `UPDATE users SET totp_secret = ?, totp_last_step = 0 WHERE id = ? AND totp_enabled = 0`, sealed, userID)
}

// EnableTOTP turns verification on (the code of step was just checked) and
// replaces the recovery codes.
func (s *Store) EnableTOTP(userID, step int64, recovery []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := execOne(tx, `UPDATE users SET totp_enabled = 1, totp_last_step = ? WHERE id = ? AND totp_secret IS NOT NULL`, step, userID); err != nil {
		return err
	}
	if err := setRecovery(tx, userID, recovery); err != nil {
		return err
	}
	return tx.Commit()
}

// DisableTOTP turns verification off and forgets the secret and codes.
func (s *Store) DisableTOTP(userID int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := execOne(tx, `UPDATE users SET totp_enabled = 0, totp_secret = NULL, totp_last_step = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// UseTOTPStep records that the code of step was used. It fails if that
// step (or a later one) was used already, so a code works only once.
func (s *Store) UseTOTPStep(userID, step int64) bool {
	res, err := s.DB.Exec(`UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, userID, step)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func recoveryHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func setRecovery(db execer, userID int64, codes []string) error {
	if _, err := db.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := db.Exec(`INSERT INTO recovery_codes(user_id, code_hash) VALUES(?, ?)`, userID, recoveryHash(c)); err != nil {
			return err
		}
	}
	return nil
}

// SetRecoveryCodes replaces the recovery codes (new set).
func (s *Store) SetRecoveryCodes(userID int64, codes []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setRecovery(tx, userID, codes); err != nil {
		return err
	}
	return tx.Commit()
}

// UseRecoveryCode consumes a recovery code; false if it is not one.
func (s *Store) UseRecoveryCode(userID int64, code string) bool {
	res, err := s.DB.Exec(`DELETE FROM recovery_codes WHERE user_id = ? AND code_hash = ?`, userID, recoveryHash(code))
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// UserIDByName is for the command line (reset of two-step verification).
func (s *Store) UserIDByName(username string) (int64, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// ---- connected devices (sessions) ----

type SessionInfo struct {
	ID        int64  `json:"id"`
	CreatedAt int64  `json:"created_at"`
	LastSeen  int64  `json:"last_seen"`
	UserAgent string `json:"user_agent"`
	IP        string `json:"ip"`
	Current   bool   `json:"current"`
}

// Sessions lists the user's active sessions, the current one marked.
func (s *Store) Sessions(userID int64, currentHash string) ([]SessionInfo, error) {
	rows, err := s.DB.Query(`SELECT rowid, created_at, last_seen, user_agent, ip, token_hash = ?
		FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY token_hash = ? DESC, last_seen DESC`,
		currentHash, userID, now(), currentHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []SessionInfo{}
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.CreatedAt, &si.LastSeen, &si.UserAgent, &si.IP, &si.Current); err != nil {
			return nil, err
		}
		list = append(list, si)
	}
	return list, rows.Err()
}

// DeleteSessionByID signs out one device of the user.
func (s *Store) DeleteSessionByID(userID, id int64) error {
	return execOne(s.DB, `DELETE FROM sessions WHERE rowid = ? AND user_id = ?`, id, userID)
}
