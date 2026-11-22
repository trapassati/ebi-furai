package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/trapassati/ebi-furai/internal/backup"
	"github.com/trapassati/ebi-furai/internal/i18n"
	"github.com/trapassati/ebi-furai/internal/webdav"
)

// Copies of the backups on a WebDAV server (Nextcloud, a NAS…), set by an
// administrator in Settings → Backup. The password is sealed in the
// database like the feed passwords and never sent back to the browser.

const (
	settingWebDAVURL  = "webdav_url"
	settingWebDAVUser = "webdav_user"
	settingWebDAVPass = "webdav_password"
)

type webdavConfig struct {
	URL  string
	User string
	Pass string
}

func (s *Server) webdavConfig() (webdavConfig, error) {
	var c webdavConfig
	c.URL, _ = s.store.Setting(settingWebDAVURL)
	c.User, _ = s.store.Setting(settingWebDAVUser)
	pass, err := s.store.SecretSetting(settingWebDAVPass)
	c.Pass = pass
	return c, err
}

// webdavRemote is the backup manager's provider of the remote folder.
func (s *Server) webdavRemote() (backup.Remote, error) {
	c, err := s.webdavConfig()
	if err != nil {
		return nil, err
	}
	if c.URL == "" {
		return nil, nil
	}
	return webdav.New(c.URL, c.User, c.Pass)
}

type webdavInfo struct {
	Configured  bool                `json:"configured"`
	URL         string              `json:"url"`
	User        string              `json:"user"`
	HasPassword bool                `json:"has_password"`
	Status      backup.RemoteStatus `json:"status"`
}

func (s *Server) webdavInfo(ctx context.Context) webdavInfo {
	c, _ := s.webdavConfig()
	info := webdavInfo{Configured: c.URL != "", URL: c.URL, User: c.User, HasPassword: c.Pass != ""}
	if s.backups != nil && info.Configured {
		info.Status = s.backups.RemoteStatus()
		if info.Status.LastError != "" {
			info.Status.LastError = i18n.T(i18n.Lang(ctx), webdavMessage(errors.New(info.Status.LastError)))
		}
	}
	return info
}

type webdavRequest struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"` // empty: keep the saved one
	Current  string `json:"current"`  // the administrator's own password (saving)
}

// client builds a client from the form, using the saved password when the
// field was left empty for the same server and user.
func (s *Server) webdavClientFrom(req webdavRequest) (*webdav.Client, string, error) {
	req.URL, req.User = strings.TrimSpace(req.URL), strings.TrimSpace(req.User)
	pass := req.Password
	if pass == "" {
		if cur, err := s.webdavConfig(); err == nil && cur.URL == req.URL && cur.User == req.User {
			pass = cur.Pass
		}
	}
	c, err := webdav.New(req.URL, req.User, pass)
	return c, pass, err
}

func webdavMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "the server did not answer in time"
	}
	return err.Error()
}

// handleTestWebDAV checks an address and credentials without saving them.
func (s *Server) handleTestWebDAV(w http.ResponseWriter, r *http.Request) {
	var req webdavRequest
	if !readJSON(w, r, &req) {
		return
	}
	c, _, err := s.webdavClientFrom(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := c.Check(ctx); err != nil {
		writeError(w, http.StatusBadRequest, webdavMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSaveWebDAV saves the settings after checking them, then copies the
// backups. Where the backups go is sensitive (they hold every account), so
// the administrator confirms with the password.
func (s *Server) handleSaveWebDAV(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusConflict, "backups are off (BACKUPS=false)")
		return
	}
	var req webdavRequest
	if !readJSON(w, r, &req) {
		return
	}
	if !s.checkPassword(w, r, req.Current) {
		return
	}
	c, pass, err := s.webdavClientFrom(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := c.Check(ctx); err != nil {
		writeError(w, http.StatusBadRequest, webdavMessage(err))
		return
	}
	for _, kv := range [][2]string{{settingWebDAVURL, c.URL()}, {settingWebDAVUser, strings.TrimSpace(req.User)}} {
		if err := s.store.SetSetting(kv[0], kv[1]); err != nil {
			serverError(w, err)
			return
		}
	}
	if err := s.store.SetSecretSetting(settingWebDAVPass, pass); err != nil {
		serverError(w, err)
		return
	}
	s.backups.ResetRemote()
	s.syncBackupsSoon()
	writeJSON(w, http.StatusOK, s.webdavInfo(r.Context()))
}

// handleDeleteWebDAV stops the copies (what is on the server stays there).
func (s *Server) handleDeleteWebDAV(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{settingWebDAVURL, settingWebDAVUser, settingWebDAVPass} {
		if err := s.store.DeleteSetting(k); err != nil {
			serverError(w, err)
			return
		}
	}
	if s.backups != nil {
		s.backups.ResetRemote()
	}
	writeJSON(w, http.StatusOK, s.webdavInfo(r.Context()))
}

// handleSyncWebDAV copies the backups now.
func (s *Server) handleSyncWebDAV(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil || !s.webdavInfo(r.Context()).Configured {
		writeError(w, http.StatusConflict, "no WebDAV folder is set")
		return
	}
	s.syncBackupsSoon()
	writeJSON(w, http.StatusAccepted, s.webdavInfo(r.Context()))
}

func (s *Server) syncBackupsSoon() {
	m := s.backups
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		m.Sync(ctx)
	}()
}

// handleDeleteBackup removes a backup from this server (its copy on the
// WebDAV server, if any, stays until the rotation removes it there).
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	}
	err := s.backups.Delete(chi.URLParam(r, "name"))
	if errors.Is(err, backup.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
