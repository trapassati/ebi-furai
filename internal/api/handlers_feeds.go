package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/trapassati/ebi-furai/internal/feeds"
	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
)

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.Sources(userID(r), s.todayStart())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func cleanName(n string) (string, bool) {
	n = strings.TrimSpace(n)
	return n, n != "" && utf8.RuneCountInString(n) <= 100
}

// ---- folders ----

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid folder name")
		return
	}
	id, err := s.store.CreateFolder(userID(r), name)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name})
}

// handleFolderOrder saves the order of the folders in the home.
func (s *Server) handleFolderOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.IDs) > 10000 {
		writeError(w, http.StatusBadRequest, "too many folders")
		return
	}
	if err := s.store.SetFolderOrder(userID(r), req.IDs); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Position *int    `json:"position"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	if req.Name != nil {
		name, ok := cleanName(*req.Name)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid folder name")
			return
		}
		if err := s.store.RenameFolder(uid, id, name); err != nil {
			storeError(w, err)
			return
		}
	}
	if req.Position != nil {
		if err := s.store.SetFolderPosition(uid, id, *req.Position); err != nil {
			storeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteFolder(userID(r), id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- feeds ----
// Fetches started by a request run to the end even if the app goes away
// meanwhile (context.WithoutCancel): an interrupted fetch would otherwise
// look like a failing feed.

// maxOPMLFeeds caps an import: a 4 MB file can list tens of thousands.
const maxOPMLFeeds = 2000

func (s *Server) handleAddFeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		FolderID int64  `json:"folder_id"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	if req.FolderID > 0 && !s.store.FolderExists(uid, req.FolderID) {
		writeError(w, http.StatusBadRequest, "no such folder")
		return
	}
	var cred *feeds.Credentials
	if u := strings.TrimSpace(req.Username); u != "" {
		if msg := credentialsProblem(feeds.NormalizeURL(req.URL), u, req.Password); msg != "" {
			writeError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		cred = &feeds.Credentials{User: u, Pass: req.Password}
	}
	cands, err := s.fetcher.Discover(r.Context(), req.URL, cred)
	if err != nil {
		// 422, not 401: a 401 would look like the app session expired.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":         discoverMessage(err),
			"auth_required": errors.Is(err, feeds.ErrAuthRequired) || errors.Is(err, feeds.ErrAuthRejected),
		})
		return
	}
	if len(cands) > 1 {
		writeJSON(w, http.StatusOK, map[string]any{"candidates": cands})
		return
	}
	c := cands[0]
	if existing, err := s.store.FeedIDByURL(uid, c.URL); err == nil {
		info, _ := s.store.FeedInfo(uid, existing)
		writeJSON(w, http.StatusConflict, map[string]any{"error": store.ErrFeedExists.Error(), "feed": info})
		return
	}
	id, err := s.store.CreateFeed(uid, c.URL, c.Title, "", req.FolderID)
	if err != nil {
		storeError(w, err)
		return
	}
	if cred != nil {
		if err := s.store.SetFeedAuth(uid, id, cred.User, cred.Pass); err != nil {
			storeError(w, err)
			return
		}
	}
	if ff, err := s.store.FetchFeedByID(uid, id); err == nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 40*time.Second)
		s.fetcher.FetchNow(ctx, []*store.FetchFeed{ff})
		cancel()
	}
	info, err := s.store.FeedInfo(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"feed": info})
}

// handleFeedURL moves a feed to a new address, for a site that changed it or
// a dead one: the address is checked like a new feed (with the feed's
// credentials, if any), then the feed is fetched at once. Articles, read
// state and stars stay.
func (s *Server) handleFeedURL(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	ff, err := s.store.FetchFeedByID(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	var cred *feeds.Credentials
	if ff.AuthUser != "" {
		if msg := credentialsProblem(feeds.NormalizeURL(req.URL), ff.AuthUser, ff.AuthPass); msg != "" {
			writeError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		cred = &feeds.Credentials{User: ff.AuthUser, Pass: ff.AuthPass}
	}
	cands, err := s.fetcher.Discover(r.Context(), req.URL, cred)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": discoverMessage(err)})
		return
	}
	if len(cands) > 1 {
		writeJSON(w, http.StatusOK, map[string]any{"candidates": cands})
		return
	}
	if other, err := s.store.FeedIDByURL(uid, cands[0].URL); err == nil && other != id {
		writeError(w, http.StatusConflict, store.ErrFeedExists.Error())
		return
	}
	if err := s.store.SetFeedURL(uid, id, cands[0].URL); err != nil {
		storeError(w, err)
		return
	}
	if ff, err := s.store.FetchFeedByID(uid, id); err == nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 40*time.Second)
		s.fetcher.FetchNow(ctx, []*store.FetchFeed{ff})
		cancel()
	}
	info, err := s.store.FeedInfo(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feed": info})
}

// credentialsProblem checks credentials before they are used: only over
// https (Basic sends the password as is) and within sane lengths.
func credentialsProblem(rawURL, user, pass string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return "for safety, username and password are only used with https addresses"
	}
	if len(user) > 200 || len(pass) > 500 || strings.ContainsAny(user, ":\r\n") {
		return "invalid username or password"
	}
	return ""
}

// handleFeedAuth sets or removes (empty username) the feed credentials,
// then fetches the feed at once to tell whether they work.
func (s *Server) handleFeedAuth(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	ff, err := s.store.FetchFeedByID(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	user := strings.TrimSpace(req.Username)
	if user != "" {
		if msg := credentialsProblem(ff.URL, user, req.Password); msg != "" {
			writeError(w, http.StatusUnprocessableEntity, msg)
			return
		}
	}
	if err := s.store.SetFeedAuth(uid, id, user, req.Password); err != nil {
		storeError(w, err)
		return
	}
	if ff, err = s.store.FetchFeedByID(uid, id); err == nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 40*time.Second)
		s.fetcher.FetchNow(ctx, []*store.FetchFeed{ff})
		cancel()
	}
	info, err := s.store.FeedInfo(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feed": info})
}

func discoverMessage(err error) string {
	switch {
	case errors.Is(err, feeds.ErrNoFeed), errors.Is(err, feeds.ErrAuthRequired), errors.Is(err, feeds.ErrAuthRejected),
		errors.Is(err, feeds.ErrYouTubeConsent):
		return err.Error()
	case errors.Is(err, netsafe.ErrBlocked):
		return "address not allowed (private or local network)"
	case errors.Is(err, context.DeadlineExceeded):
		return "the site did not respond in time"
	}
	return "cannot read the address: " + err.Error()
}

func (s *Server) handleUpdateFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Title      *string `json:"title"`
		FolderID   *int64  `json:"folder_id"`   // 0 = no folder
		FullText   *bool   `json:"full_text"`   // download the full text of new articles
		OpenWeb    *bool   `json:"open_web"`    // open articles on the site
		HideShorts *bool   `json:"hide_shorts"` // YouTube: hide the Shorts
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if utf8.RuneCountInString(t) > 200 {
			writeError(w, http.StatusBadRequest, "title too long")
			return
		}
		if err := s.store.RenameFeed(uid, id, t); err != nil {
			storeError(w, err)
			return
		}
	}
	if req.FolderID != nil {
		if err := s.store.MoveFeed(uid, id, *req.FolderID); err != nil {
			storeError(w, err)
			return
		}
	}
	if req.FullText != nil {
		if err := s.store.SetFeedFullText(uid, id, *req.FullText); err != nil {
			storeError(w, err)
			return
		}
		if *req.FullText {
			s.fetcher.WakeFullText()
		}
	}
	if req.OpenWeb != nil {
		if err := s.store.SetFeedOpenWeb(uid, id, *req.OpenWeb); err != nil {
			storeError(w, err)
			return
		}
	}
	if req.HideShorts != nil {
		if err := s.store.SetFeedHideShorts(uid, id, *req.HideShorts); err != nil {
			storeError(w, err)
			return
		}
	}
	info, err := s.store.FeedInfo(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feed": info})
}

func (s *Server) handleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteFeed(userID(r), id); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRefreshFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid := userID(r)
	ff, err := s.store.FetchFeedByID(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	ff.ErrorCount = 0 // manual refresh: ignore backoff and validators
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 40*time.Second)
	defer cancel()
	s.fetcher.FetchNow(ctx, []*store.FetchFeed{ff})
	info, err := s.store.FeedInfo(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feed": info})
}

func (s *Server) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.UserFetchFeeds(userID(r))
	if err != nil {
		serverError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 45*time.Second)
	defer cancel()
	failed := s.fetcher.FetchNow(ctx, list)
	writeJSON(w, http.StatusOK, map[string]int{"feeds": len(list), "failed": failed})
}

func (s *Server) handleFeedIcon(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	data, typ, err := s.store.FeedIcon(userID(r), id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	w.Write(data)
}

// ---- OPML ----

func (s *Server) handleExportOPML(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ExportFeeds(userID(r))
	if err != nil {
		serverError(w, err)
		return
	}
	body, err := feeds.BuildOPML(s.cfg.AppName+" – "+sessionFrom(r).User.Username, list)
	if err != nil {
		serverError(w, err)
		return
	}
	name := fmt.Sprintf("feeds-%s.opml", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(body)
}

func (s *Server) handleImportOPML(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOPMLBody)
	var src io.Reader = r.Body
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing file")
			return
		}
		defer file.Close()
		src = file
	}
	entries, err := feeds.ParseOPML(src)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(entries) > maxOPMLFeeds {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("the file has too many feeds (at most %d)", maxOPMLFeeds))
		return
	}
	uid := userID(r)
	folders := map[string]int64{}
	added, skipped, invalid := 0, 0, 0
	for _, e := range entries {
		if _, err := netsafe.ValidateURL(e.URL); err != nil {
			invalid++
			continue
		}
		var folderID int64
		if name, ok := cleanName(e.Folder); ok {
			if id, ok := folders[name]; ok {
				folderID = id
			} else {
				id, err := s.store.FolderByName(uid, name)
				if err != nil {
					serverError(w, err)
					return
				}
				folders[name] = id
				folderID = id
			}
		}
		_, err := s.store.CreateFeed(uid, e.URL, e.Title, "", folderID)
		switch {
		case errors.Is(err, store.ErrFeedExists):
			skipped++
		case err != nil:
			serverError(w, err)
			return
		default:
			added++
		}
	}
	s.fetcher.Wake()
	writeJSON(w, http.StatusOK, map[string]int{"added": added, "skipped": skipped, "invalid": invalid})
}
