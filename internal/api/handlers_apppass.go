package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strings"

	"github.com/trapassati/ebi-furai/internal/auth"
	"github.com/trapassati/ebi-furai/internal/store"
)

// App passwords: what apps that sync through the Google Reader API sign in
// with. They are random (about 100 bits), so a SHA-256 is enough to store
// them, and they work with two-step verification on.

const appPassAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no 0/o, 1/l/i

// newAppPassword returns a secret like "k3xmp-9qrta-vw2hc-e7nfs".
func newAppPassword() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var sb strings.Builder
	for i, c := range b {
		if i > 0 && i%5 == 0 {
			sb.WriteByte('-')
		}
		// 256 = 8*31 + 8: the small bias does not matter at 100 bits.
		sb.WriteByte(appPassAlphabet[int(c)%len(appPassAlphabet)])
	}
	return sb.String()
}

// appPasswordHash normalizes what an app sends (spaces, dashes, capitals)
// and hashes it.
func appPasswordHash(secret string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return r
	}, secret)
	return auth.HashToken("app:" + s)
}

func (s *Server) handleAppPasswords(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.AppPasswords(userID(r))
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleCreateAppPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "give it a name, for example the app it is for")
		return
	}
	if !s.checkPassword(w, r, req.Password) {
		return
	}
	secret := newAppPassword()
	p, err := s.store.CreateAppPassword(userID(r), req.Name, appPasswordHash(secret))
	if errors.Is(err, store.ErrTooManyAppPasswords) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": p, "secret": secret})
}

func (s *Server) handleDeleteAppPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAppPassword(userID(r), id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
