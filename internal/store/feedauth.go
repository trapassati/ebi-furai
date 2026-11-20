package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// Feed passwords are stored sealed (AES-256-GCM) with a random key kept in
// the settings table: a copied database or backup alone does not show them
// in clear text, and they are never sent back to the browser.

func (s *Store) feedAEAD() (cipher.AEAD, error) {
	s.authOnce.Do(func() {
		key, err := s.SecretKey("feed_auth_key")
		if err != nil {
			s.authErr = err
			return
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			s.authErr = err
			return
		}
		s.authAEAD, s.authErr = cipher.NewGCM(block)
	})
	return s.authAEAD, s.authErr
}

func (s *Store) sealPassword(feedID int64, pass string) ([]byte, error) {
	aead, err := s.feedAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// The feed id is bound as additional data: a secret cannot be moved to another feed.
	return aead.Seal(nonce, nonce, []byte(pass), feedAAD(feedID)), nil
}

func (s *Store) openPassword(feedID int64, sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	aead, err := s.feedAEAD()
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", errors.New("damaged credentials")
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], feedAAD(feedID))
	if err != nil {
		return "", errors.New("unreadable credentials")
	}
	return string(plain), nil
}

func feedAAD(id int64) []byte {
	b := make([]byte, 8)
	for i := range 8 {
		b[i] = byte(id >> (8 * i))
	}
	return append([]byte("feed-auth:"), b...)
}

// SetFeedAuth saves (or with an empty user, removes) the feed credentials.
func (s *Store) SetFeedAuth(userID, feedID int64, user, pass string) error {
	var sealed []byte
	if user != "" {
		var err error
		if sealed, err = s.sealPassword(feedID, pass); err != nil {
			return err
		}
	}
	res, err := s.DB.Exec(`UPDATE feeds SET auth_user = ?, auth_secret = ?, etag = '', last_modified = '', next_fetch_at = 0
		WHERE id = ? AND user_id = ?`, user, sealed, feedID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
