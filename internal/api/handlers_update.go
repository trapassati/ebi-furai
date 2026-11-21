package api

import (
	"net/http"

	"github.com/trapassati/ebi-furai/internal/update"
)

// SetUpdates gives the server the update checker (nil = turned off).
func (s *Server) SetUpdates(c *update.Checker) { s.updates = c }

// handleUpdate tells the administrators whether a newer release is out.
func (s *Server) handleUpdate(w http.ResponseWriter, _ *http.Request) {
	if s.updates == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "available": false, "current": Release})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "update": s.updates.Status(),
		"available": s.updates.Status().Available})
}
