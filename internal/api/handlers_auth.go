package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/trapassati/ebi-furai/internal/auth"
	"github.com/trapassati/ebi-furai/internal/store"
)

type meResponse struct {
	User    *store.User     `json:"user"`
	Prefs   json.RawMessage `json:"prefs"`
	CSRF    string          `json:"csrf"`
	AppName string          `json:"app_name"`
	Version string          `json:"version"`
	// Server default refresh interval (REFRESH_INTERVAL), in minutes.
	DefaultRefresh int `json:"default_refresh_minutes"`
	// Two-step verification of the account, and recovery codes left.
	TOTP         bool `json:"totp_enabled"`
	RecoveryLeft int  `json:"recovery_left"`
}

func (s *Server) me(sess *store.Session) meResponse {
	u := *sess.User
	prefs := u.Prefs
	u.Prefs = nil
	if len(prefs) == 0 {
		prefs = json.RawMessage("{}")
	}
	out := meResponse{User: &u, Prefs: prefs, CSRF: sess.CSRF, AppName: s.cfg.AppName, Version: s.static.Version,
		DefaultRefresh: int(s.cfg.RefreshInterval / time.Minute)}
	if st, err := s.store.TOTP(u.ID); err == nil {
		out.TOTP, out.RecoveryLeft = st.Enabled, st.RecoveryCodes
	}
	return out
}

// handleSetRefresh stores how often the user's feeds are refreshed.
func (s *Server) handleSetRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Minutes int `json:"minutes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !slices.Contains(store.RefreshChoices, req.Minutes) {
		writeError(w, http.StatusBadRequest, "invalid interval")
		return
	}
	if err := s.store.SetRefreshMinutes(userID(r), req.Minutes, s.fetcher.Interval(req.Minutes)); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"refresh_minutes": req.Minutes})
}

// handleSetDedup turns the recognition of duplicates on or off.
func (s *Server) handleSetDedup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.store.SetDedup(userID(r), req.On); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"dedup": req.On})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	ip := s.clientIP(r)
	if !s.limiter.Allowed(ip, req.Username) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a few minutes")
		return
	}
	user, hash, err := s.store.UserForLogin(req.Username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		serverError(w, err)
		return
	}
	ok := false
	if user != nil {
		ok, _ = auth.VerifyPassword(req.Password, hash)
	} else {
		auth.BurnTime(req.Password)
	}
	if !ok || user.Disabled {
		s.limiter.Fail(ip, req.Username)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.limiter.Success(ip)

	// With two-step verification the password alone does not open a
	// session: a short-lived ticket waits for the code (handleLoginTOTP).
	st, err := s.store.TOTP(user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if st.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"totp_required": true, "ticket": s.tickets.put(user.ID)})
		return
	}
	s.startSession(w, r, user)
}

// startSession signs the user in on this device.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *store.User) {
	token := auth.RandomToken(32)
	csrf := auth.RandomToken(24)
	if err := s.store.CreateSession(auth.HashToken(token), user.ID, csrf, r.UserAgent(), s.clientIP(r), sessionTTL); err != nil {
		serverError(w, err)
		return
	}
	s.setCookie(w, r, token)
	writeJSON(w, http.StatusOK, s.me(&store.Session{User: user, CSRF: csrf}))
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.me(sessionFrom(r)))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	hash, _ := r.Context().Value(ctxToken).(string)
	s.store.DeleteSession(hash)
	s.clearCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	if !s.checkPassword(w, r, req.Current) {
		return
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newHash, err := auth.HashPassword(req.New)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.store.SetPassword(uid, newHash); err != nil {
		storeError(w, err)
		return
	}
	// Log out the other devices, keep the current session.
	current, _ := r.Context().Value(ctxToken).(string)
	s.store.DeleteUserSessions(uid, current)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePrefs(w http.ResponseWriter, r *http.Request) {
	var prefs map[string]any
	if !readJSON(w, r, &prefs) {
		return
	}
	b, err := json.Marshal(prefs)
	if err != nil || len(b) > 4096 {
		writeError(w, http.StatusBadRequest, "invalid preferences")
		return
	}
	if err := s.store.SetPrefs(userID(r), b); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(b))
}

// ---- admin ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListUsers()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}

func validUsername(u string) bool {
	if len(u) < 2 || len(u) > 64 {
		return false
	}
	for _, c := range u {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-@", c)) {
			return false
		}
	}
	return true
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) {
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
	u, err := s.store.CreateUser(req.Username, hash, req.IsAdmin)
	if err != nil {
		storeError(w, err)
		return
	}
	u.Prefs = nil
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
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
	if err := s.store.SetPassword(id, hash); err != nil {
		storeError(w, err)
		return
	}
	keep := ""
	if id == userID(r) {
		keep, _ = r.Context().Value(ctxToken).(string)
	}
	s.store.DeleteUserSessions(id, keep)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if id == userID(r) {
			writeError(w, http.StatusBadRequest, "you cannot disable yourself")
			return
		}
		if err := s.store.SetDisabled(id, disabled); err != nil {
			storeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if id == userID(r) {
		writeError(w, http.StatusBadRequest, "you cannot delete yourself")
		return
	}
	if err := s.store.DeleteUser(id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
