package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trapassati/ebi-furai/internal/backup"
)

// SetBackups enables the backup pages (admin only).
func (s *Server) SetBackups(m *backup.Manager) { s.backups = m }

type backupList struct {
	Enabled bool          `json:"enabled"`
	Dir     string        `json:"dir"`
	Daily   int           `json:"keep_daily"`
	Weekly  int           `json:"keep_weekly"`
	Items   []backup.Info `json:"items"`
}

func (s *Server) handleListBackups(w http.ResponseWriter, _ *http.Request) {
	out := backupList{Items: []backup.Info{}, Daily: s.cfg.BackupDaily, Weekly: s.cfg.BackupWeekly}
	if s.backups == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	list, err := s.backups.List()
	if err != nil {
		serverError(w, err)
		return
	}
	out.Enabled, out.Dir, out.Items = true, s.backups.Dir(), list
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusConflict, "backups are off (BACKUPS=false)")
		return
	}
	info, err := s.backups.Create(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

// handleDownloadBackup sends a backup file. It holds every account, the
// sealed feed passwords and the key that opens them: admin only, never cached.
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	}
	f, info, err := s.backups.Open(chi.URLParam(r, "name"))
	if errors.Is(err, backup.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+info.Name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name, info.Time, f)
}
