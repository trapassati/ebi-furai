package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/trapassati/ebi-furai/internal/push"
	"github.com/trapassati/ebi-furai/internal/store"
)

const maxDigestTimes = 8

func (s *Server) handlePushInfo(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	settings, err := s.store.NotifySettings(uid)
	if err != nil {
		serverError(w, err)
		return
	}
	subs, err := s.store.PushSubscriptions(uid)
	if err != nil {
		serverError(w, err)
		return
	}
	current := r.URL.Query().Get("endpoint")
	devices := make([]map[string]any, 0, len(subs))
	for _, sub := range subs {
		devices = append(devices, map[string]any{
			"id": sub.ID, "device": sub.Device, "created_at": sub.CreatedAt, "last_ok_at": sub.LastOKAt,
			"fail_count": sub.FailCount, "last_error": sub.LastError, "current": current != "" && sub.Endpoint == current,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"public_key": s.push.PublicKey(),
		"settings":   settings,
		"devices":    devices,
		"timezone":   s.cfg.Location.String(),
	})
}

func (s *Server) handlePushSettings(w http.ResponseWriter, r *http.Request) {
	var req store.NotifySettings
	if !readJSON(w, r, &req) {
		return
	}
	seen := map[string]bool{}
	times := []string{}
	for _, t := range req.Times {
		if _, ok := push.ParseHHMM(t); !ok {
			writeError(w, http.StatusBadRequest, "invalid time: "+t)
			return
		}
		if !seen[t] {
			seen[t] = true
			times = append(times, t)
		}
	}
	if len(times) > maxDigestTimes {
		writeError(w, http.StatusBadRequest, "at most 8 times a day")
		return
	}
	slices.Sort(times)
	req.Times = times
	_, ok1 := push.ParseHHMM(req.QuietStart)
	_, ok2 := push.ParseHHMM(req.QuietEnd)
	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "invalid quiet hours")
		return
	}
	if err := s.store.SaveNotifySettings(userID(r), &req); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Device string `json:"device"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !push.ValidEndpoint(req.Endpoint) {
		writeError(w, http.StatusBadRequest, "push service not recognized")
		return
	}
	if req.Keys.P256dh == "" || req.Keys.Auth == "" || len(req.Keys.P256dh) > 200 || len(req.Keys.Auth) > 100 {
		writeError(w, http.StatusBadRequest, "subscription keys missing")
		return
	}
	// Reject keys that cannot be used, before storing them.
	if _, err := push.Encrypt(push.Subscription{Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth}, []byte("{}")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscription keys")
		return
	}
	device := strings.TrimSpace(req.Device)
	if device == "" {
		device = "Dispositivo"
	}
	id, err := s.store.SavePushSubscription(userID(r), req.Endpoint, req.Keys.P256dh, req.Keys.Auth, device)
	if err != nil {
		serverError(w, err)
		return
	}
	s.push.RememberOrigin(r.Header.Get("Origin"))
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.store.DeletePushEndpoint(userID(r), req.Endpoint); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeletePushSubscription(userID(r), id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	res, err := s.push.SendDigest(ctx, userID(r), true)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
