package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/trapassati/ebi-furai/internal/backup"
	"github.com/trapassati/ebi-furai/internal/db"
)

// SetBackups enables the backup pages (admin only).
func (s *Server) SetBackups(m *backup.Manager) {
	s.backups = m
	m.SetRemote(s.webdavRemote)
}

type backupList struct {
	Enabled bool          `json:"enabled"`
	Dir     string        `json:"dir"`
	Daily   int           `json:"keep_daily"`
	Weekly  int           `json:"keep_weekly"`
	Items   []backup.Info `json:"items"`
	WebDAV  webdavInfo    `json:"webdav"`
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
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
	out.WebDAV = s.webdavInfo(r.Context())
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

// SetRestart gives the server a way to restart itself, to apply a restored
// database (set by main).
func (s *Server) SetRestart(fn func()) { s.restart = fn }

// maxRestoreUpload limits an uploaded backup (compressed).
const maxRestoreUpload = 2 << 30

// handleRestoreBackup replaces the database with one of the backups in the
// folder, or with an uploaded file (multipart "file"). The administrator
// confirms with the password. The current database is saved first, then
// the server restarts and everyone signs in again.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil || s.restart == nil {
		writeError(w, http.StatusConflict, "backups are off (BACKUPS=false)")
		return
	}
	name := chi.URLParam(r, "name")
	var password string
	var upload io.ReadCloser
	if name == "" {
		// A large file over a slow connection takes time.
		rc := http.NewResponseController(w)
		rc.SetReadDeadline(time.Now().Add(20 * time.Minute))
		rc.SetWriteDeadline(time.Now().Add(25 * time.Minute))
		r.Body = http.MaxBytesReader(w, r.Body, maxRestoreUpload)
		mr, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing file")
			return
		}
		// The password comes first, so a wrong one stops before the upload.
		for {
			part, err := mr.NextPart()
			if err != nil {
				writeError(w, http.StatusBadRequest, "missing file")
				return
			}
			if part.FormName() == "password" {
				b, _ := io.ReadAll(io.LimitReader(part, 1024))
				password = string(b)
				continue
			}
			if part.FormName() == "file" {
				upload = part
				break
			}
		}
	} else {
		var req struct {
			Password string `json:"password"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		password = req.Password
	}
	if !s.checkPassword(w, r, password) {
		return
	}
	s.restoring.Lock()
	defer s.restoring.Unlock()
	if backup.Pending(s.cfg.DBPath()) {
		writeError(w, http.StatusConflict, "a restore is already in progress")
		return
	}
	// First a copy of things as they are now, to go back if needed.
	safety, err := s.backups.SafetyCopy(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var info db.Info
	if upload != nil {
		info, err = backup.Stage(upload, s.cfg.DBPath())
	} else {
		info, err = s.backups.StageBackup(name, s.cfg.DBPath())
	}
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case err != nil && refusal(err) != nil:
		// Only the reason, not SQLite's details about the damage.
		writeError(w, http.StatusBadRequest, refusal(err).Error())
		return
	case err != nil:
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, backup.ErrTooLarge.Error())
			return
		}
		serverError(w, err)
		return
	}
	slog.Warn("database restore staged: restarting", "by", userID(r), "safety_copy", safety.Name,
		"users", info.Users, "feeds", info.Feeds, "articles", info.Articles)
	writeJSON(w, http.StatusAccepted, map[string]any{"restarting": true, "safety_copy": safety.Name, "restored": info})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(300 * time.Millisecond) // let the answer leave
		s.restart()
	}()
}

// refusal returns the reason a backup cannot be restored, or nil when the
// error is not about the file.
func refusal(err error) error {
	for _, e := range []error{db.ErrNotDatabase, db.ErrDamaged, db.ErrTooNew, db.ErrNoAdmin, backup.ErrTooLarge} {
		if errors.Is(err, e) {
			return e
		}
	}
	return nil
}
