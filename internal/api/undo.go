package api

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// undoTTL is how long a "mark all as read" can be undone.
const undoTTL = 2 * time.Minute

// undoStore keeps, for a short time, the articles changed by the latest
// "mark all as read" operations, behind a random token.
type undoStore struct {
	mu      sync.Mutex
	entries map[string]undoEntry
}

type undoEntry struct {
	userID  int64
	ids     []int64
	expires time.Time
}

func (u *undoStore) put(userID int64, ids []int64) string {
	b := make([]byte, 18)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.entries == nil {
		u.entries = map[string]undoEntry{}
	}
	for k, e := range u.entries {
		if now.After(e.expires) {
			delete(u.entries, k)
		}
	}
	u.entries[token] = undoEntry{userID: userID, ids: ids, expires: now.Add(undoTTL)}
	return token
}

// take returns the articles of a token, once, only to its owner.
func (u *undoStore) take(userID int64, token string) ([]int64, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.entries[token]
	if !ok || e.userID != userID || time.Now().After(e.expires) {
		return nil, false
	}
	delete(u.entries, token)
	return e.ids, true
}
