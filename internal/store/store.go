// Package store contains all database queries.
package store

import (
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	DB *sql.DB

	authOnce sync.Once
	authAEAD cipher.AEAD
	authErr  error
}

func New(db *sql.DB) *Store { return &Store{DB: db} }

func now() int64 { return time.Now().Unix() }

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Setting returns a stored setting value.
func (s *Store) Setting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SecretKey returns a persistent random key, creating it on first use.
func (s *Store) SecretKey(name string) ([]byte, error) {
	v, err := s.Setting(name)
	if err == nil {
		return hex.DecodeString(v)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := s.SetSetting(name, hex.EncodeToString(b)); err != nil {
		return nil, err
	}
	return b, nil
}
