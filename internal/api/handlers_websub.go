package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trapassati/ebi-furai/internal/store"
	"github.com/trapassati/ebi-furai/internal/websub"
)

// SetWebSub turns on instant updates (unless PUBLIC_URL=off).
func (s *Server) SetWebSub(m *websub.Manager) { s.websub = m }

// handleWebSub receives the calls of the WebSub hubs.
func (s *Server) handleWebSub(w http.ResponseWriter, r *http.Request) {
	if s.websub == nil {
		http.NotFound(w, r)
		return
	}
	s.websub.Handle(w, r, chi.URLParam(r, "token"))
}

// websubStatus is the administration page of the instant updates.
type websubStatus struct {
	Mode      string `json:"mode"` // "learned", "fixed" or "off" (PUBLIC_URL=off)
	PublicURL string `json:"public_url"`
	*store.WebSubReport
}

func (s *Server) websubStatus(adminID int64) (*websubStatus, error) {
	st := &websubStatus{Mode: "off"}
	if s.websub != nil {
		st.Mode = "learned"
		if s.websub.Fixed() {
			st.Mode = "fixed"
		}
		st.PublicURL = s.websub.Base()
	}
	rep, err := s.store.WebSubReport(adminID)
	if err != nil {
		return nil, err
	}
	st.WebSubReport = rep
	return st, nil
}

func (s *Server) handleWebSubStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.websubStatus(sessionFrom(r).User.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleWebSubRetry asks the hubs again for the subscriptions that failed.
func (s *Server) handleWebSubRetry(w http.ResponseWriter, r *http.Request) {
	if s.websub == nil {
		writeError(w, http.StatusConflict, "instant updates are turned off (PUBLIC_URL=off)")
		return
	}
	if err := s.websub.RetryFailed(); err != nil {
		serverError(w, err)
		return
	}
	s.handleWebSubStatus(w, r)
}
