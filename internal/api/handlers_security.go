package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/trapassati/ebi-furai/internal/auth"
)

// checkPassword asks the current password again before a sensitive change
// (rate limited like the login, so a stolen session cannot guess it). It
// writes the error response itself and returns false on failure.
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	uid := userID(r)
	ip, key := s.clientIP(r), "#password:"+strconv.FormatInt(uid, 10)
	if !s.limiter.Allowed(ip, key) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a few minutes")
		return false
	}
	hash, err := s.store.PasswordHash(uid)
	if err != nil {
		storeError(w, err)
		return false
	}
	if ok, _ := auth.VerifyPassword(password, hash); !ok {
		s.limiter.Fail(ip, key)
		writeError(w, http.StatusBadRequest, "the current password is not correct")
		return false
	}
	return true
}

// ---- login, second step ----

// ticketStore holds the logins waiting for the code: a ticket lasts five
// minutes and allows five wrong codes.
type ticketStore struct {
	mu      sync.Mutex
	entries map[string]*ticket
}

type ticket struct {
	userID  int64
	expires time.Time
	tries   int
}

const ticketTTL = 5 * time.Minute

func (t *ticketStore) put(userID int64) string {
	b := make([]byte, 24)
	rand.Read(b)
	tok := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entries == nil {
		t.entries = map[string]*ticket{}
	}
	for k, e := range t.entries {
		if now.After(e.expires) {
			delete(t.entries, k)
		}
	}
	t.entries[tok] = &ticket{userID: userID, expires: now.Add(ticketTTL)}
	return tok
}

func (t *ticketStore) user(tok string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[tok]
	if !ok || time.Now().After(e.expires) {
		return 0, false
	}
	return e.userID, true
}

// fail counts a wrong code; after five the ticket is gone.
func (t *ticketStore) fail(tok string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.entries[tok]; ok {
		if e.tries++; e.tries >= 5 {
			delete(t.entries, tok)
		}
	}
}

func (t *ticketStore) done(tok string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, tok)
}

// verifyCode accepts a code of the authenticator app (once: the same code
// cannot be used twice) or a recovery code (consumed).
func (s *Server) verifyCode(uid int64, code string) (ok, recovery bool) {
	st, err := s.store.TOTP(uid)
	if err != nil || st.Secret == nil {
		return false, false
	}
	if step, ok := auth.VerifyTOTP(st.Secret, code, time.Now()); ok {
		return s.store.UseTOTPStep(uid, step), false
	}
	if rc := auth.NormalizeRecoveryCode(code); rc != "" && st.Enabled && s.store.UseRecoveryCode(uid, rc) {
		return true, true
	}
	return false, false
}

func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid, ok := s.tickets.user(req.Ticket)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sign-in expired: enter the password again", "restart": true})
		return
	}
	ip, key := s.clientIP(r), "#totp:"+strconv.FormatInt(uid, 10)
	if !s.limiter.Allowed(ip, key) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a few minutes")
		return
	}
	ok, _ = s.verifyCode(uid, req.Code)
	if !ok {
		s.limiter.Fail(ip, key)
		s.tickets.fail(req.Ticket)
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}
	s.tickets.done(req.Ticket)
	s.limiter.Success(ip)
	user, err := s.store.UserByID(uid)
	if err != nil || user.Disabled {
		writeError(w, http.StatusBadRequest, "invalid credentials")
		return
	}
	s.startSession(w, r, user)
}

// ---- settings: two-step verification ----

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) || !s.checkPassword(w, r, req.Password) {
		return
	}
	uid := userID(r)
	if st, err := s.store.TOTP(uid); err != nil {
		serverError(w, err)
		return
	} else if st.Enabled {
		writeError(w, http.StatusConflict, "two-step verification is already on")
		return
	}
	secret := auth.NewTOTPSecret()
	if err := s.store.SetTOTPPending(uid, secret); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": auth.TOTPSecretText(secret),
		"uri":    auth.TOTPURI(s.cfg.AppName, sessionFrom(r).User.Username, secret),
	})
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	ip, key := s.clientIP(r), "#totp:"+strconv.FormatInt(uid, 10)
	if !s.limiter.Allowed(ip, key) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a few minutes")
		return
	}
	st, err := s.store.TOTP(uid)
	if err != nil {
		serverError(w, err)
		return
	}
	if st.Enabled || st.Secret == nil {
		writeError(w, http.StatusConflict, "start the setup again")
		return
	}
	step, ok := auth.VerifyTOTP(st.Secret, req.Code, time.Now())
	if !ok {
		s.limiter.Fail(ip, key)
		writeError(w, http.StatusBadRequest, "invalid code: check the phone's clock and try again")
		return
	}
	codes := auth.NewRecoveryCodes(10)
	if err := s.store.EnableTOTP(uid, step, codes); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) || !s.checkPassword(w, r, req.Password) {
		return
	}
	if err := s.store.DisableTOTP(userID(r)); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) || !s.checkPassword(w, r, req.Password) {
		return
	}
	uid := userID(r)
	if st, err := s.store.TOTP(uid); err != nil || !st.Enabled {
		writeError(w, http.StatusConflict, "two-step verification is not on")
		return
	}
	codes := auth.NewRecoveryCodes(10)
	if err := s.store.SetRecoveryCodes(uid, codes); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// handleAdminDisableTOTP is for a user who lost the phone and the codes.
func (s *Server) handleAdminDisableTOTP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DisableTOTP(id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- connected devices ----

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	current, _ := r.Context().Value(ctxToken).(string)
	list, err := s.store.Sessions(userID(r), current)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSessionByID(userID(r), id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogoutOthers(w http.ResponseWriter, r *http.Request) {
	current, _ := r.Context().Value(ctxToken).(string)
	if err := s.store.DeleteUserSessions(userID(r), current); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
