package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"github.com/trapassati/ebi-furai/internal/auth"
)

// First start without ADMIN_USER/ADMIN_PASSWORD: the app shows a page to
// create the administrator. To be sure it is the owner, and not whoever
// reaches the server first, the page asks for a code that the server
// prints in its log at start (docker compose logs).
type setupState struct {
	mu   sync.Mutex
	code string // "" when there is nothing to set up
}

// SetSetupCode enables the setup page with a code (from main, when the
// database has no users).
func (s *Server) SetSetupCode(code string) {
	s.setup.mu.Lock()
	s.setup.code = code
	s.setup.mu.Unlock()
}

func (s *Server) setupNeeded() bool {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	return s.setup.code != ""
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"needed": s.setupNeeded()})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ip := s.clientIP(r)
	if !s.limiter.Allowed(ip, "setup") {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a few minutes")
		return
	}
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	if s.setup.code == "" {
		writeError(w, http.StatusConflict, "the administrator already exists: sign in")
		return
	}
	code := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(req.Code)))
	if subtle.ConstantTimeCompare([]byte(code), []byte(s.setup.code)) != 1 {
		s.limiter.Fail(ip, "setup")
		writeError(w, http.StatusForbidden, "wrong setup code: you find it in the server log")
		return
	}
	name := strings.TrimSpace(req.Username)
	if !validUsername(name) {
		writeError(w, http.StatusBadRequest, "invalid username (2-64 characters: letters, digits, . _ - @)")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		serverError(w, err)
		return
	}
	n, err := s.store.CountUsers()
	if err != nil {
		serverError(w, err)
		return
	}
	if n > 0 {
		s.setup.code = ""
		writeError(w, http.StatusConflict, "the administrator already exists: sign in")
		return
	}
	user, err := s.store.CreateUser(name, hash, true)
	if err != nil {
		storeError(w, err)
		return
	}
	s.setup.code = ""
	s.limiter.Success(ip)
	s.startSession(w, r, user)
}
