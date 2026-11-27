package api

import (
	"net/http"
	"strconv"
)

// handleStats returns the statistics of the user's feeds: opens per day,
// and per feed what arrived and what was opened. ?days= is 30 or 90.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days != 90 {
		days = 30
	}
	st, err := s.store.FeedStats(userID(r), days)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
