package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleAdFilterStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ads.Status())
}

// handleAdFilterSet turns the filter on or off for every user (admin).
func (s *Server) handleAdFilterSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	v := "0"
	if req.Enabled {
		v = "1"
	}
	if err := s.store.SetSetting("ad_filter", v); err != nil {
		writeError(w, http.StatusInternalServerError, "setting not saved")
		return
	}
	s.adOn.Store(req.Enabled)
	writeJSON(w, http.StatusOK, s.ads.Status())
}

// handleAdFilterUpdate downloads the lists now (admin).
func (s *Server) handleAdFilterUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 80*time.Second)
	defer cancel()
	err := s.ads.Update(ctx)
	st := s.ads.Status()
	if err != nil && st.Rules == 0 {
		writeError(w, http.StatusBadGateway, "lists not downloaded: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}
