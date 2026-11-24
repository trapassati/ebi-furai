package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trapassati/ebi-furai/internal/websub"
)

// SetWebSub turns on instant updates (PUBLIC_URL is set).
func (s *Server) SetWebSub(m *websub.Manager) { s.websub = m }

// handleWebSub receives the calls of the WebSub hubs.
func (s *Server) handleWebSub(w http.ResponseWriter, r *http.Request) {
	if s.websub == nil {
		http.NotFound(w, r)
		return
	}
	s.websub.Handle(w, r, chi.URLParam(r, "token"))
}
