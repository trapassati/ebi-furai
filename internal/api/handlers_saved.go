package api

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/dedup"
	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
)

// "Read later": a page saved from the app, the browser (the bookmarklet
// opens the app, signed in) or the iPhone's share sheet (a Shortcut that
// posts the address with an app password).

const maxSaveBody = 8 << 10

// savePage downloads and stores a page. When it cannot be read the link is
// kept all the same, with the reason, so that it can be opened on the site
// or read again later.
func (s *Server) savePage(ctx context.Context, userID int64, raw string) (id int64, title, problem string, again bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p := store.SavedPage{URL: raw}
	page, ferr := content.FetchPage(ctx, s.fetcher.Client(), raw)
	if ferr != nil {
		problem = ferr.Error()
		switch {
		case errors.Is(ferr, netsafe.ErrBlocked):
			problem = "address not allowed"
		case errors.Is(ferr, context.DeadlineExceeded):
			problem = "the site did not respond in time"
		}
	} else {
		p.Title = page.Title
		p.Author = truncateRunes(page.Byline, 200)
		p.Content = page.HTML
		p.Text = content.PlainText(page.HTML)
		if strings.TrimSpace(p.Text) == "" && page.Excerpt != "" {
			p.Content = "<p>" + htmlEscape(page.Excerpt) + "</p>"
			p.Text = page.Excerpt
		}
		p.Preview = page.Excerpt
		if p.Preview == "" {
			p.Preview = content.Truncate(p.Text, 280)
		}
		p.Thumbnail = page.Image
		if p.Thumbnail == "" {
			p.Thumbnail = content.FirstImage(p.Content)
		}
		p.Words = dedup.Words(p.Text)
	}
	if p.Title == "" {
		p.Title = linkTitle(raw)
	}
	id, again, err = s.store.SavePage(userID, p)
	return id, p.Title, problem, again, err
}

// saveURL checks an address given to save.
func saveURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	// The share sheet may give "Title https://…": keep the link.
	if i := strings.LastIndex(raw, "http"); i > 0 && strings.ContainsAny(raw[:i], " \n") {
		raw = strings.TrimSpace(raw[i:])
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 2048 {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

// linkTitle: the address without the scheme, when the page has no title.
func linkTitle(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	t := u.Host + strings.TrimSuffix(u.EscapedPath(), "/")
	if p, err := url.PathUnescape(t); err == nil {
		t = p
	}
	return truncateRunes(strings.TrimPrefix(t, "www."), 200)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// handleSavePage: from the app (signed in).
func (s *Server) handleSavePage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	link, ok := saveURL(req.URL)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not a web address")
		return
	}
	uid := userID(r)
	id, title, problem, again, err := s.savePage(r.Context(), uid, link)
	if err != nil {
		serverError(w, err)
		return
	}
	a, err := s.store.GetArticle(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	out := map[string]any{"article": s.articleJSON(a), "title": title, "again": again}
	if problem != "" {
		out["problem"] = problem
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSaveWithKey: from a Shortcut or another app, with an app password
// as a bearer token. The address comes as JSON ({"url": …}), as a form
// field "url" or as plain text.
func (s *Server) handleSaveWithKey(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	secret = strings.TrimSpace(secret)
	if !ok || secret == "" {
		writeError(w, http.StatusUnauthorized, "an app password is needed, as \"Authorization: Bearer <app password>\"")
		return
	}
	if !s.limiter.Allowed(ip, "save:"+ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a few minutes")
		return
	}
	uid, pid, err := s.store.AppPasswordUser(appPasswordHash(secret))
	if errors.Is(err, store.ErrNotFound) {
		s.limiter.Fail(ip, "save:"+ip)
		writeError(w, http.StatusUnauthorized, "wrong app password")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.limiter.Success(ip)
	s.store.TouchAppPassword(pid, ip)

	var raw string
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	r.Body = http.MaxBytesReader(w, r.Body, maxSaveBody)
	switch mt {
	case "application/json":
		var req struct {
			URL string `json:"url"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		raw = req.URL
	case "application/x-www-form-urlencoded", "multipart/form-data":
		if err := r.ParseMultipartForm(maxSaveBody); err != nil && !errors.Is(err, http.ErrNotMultipart) {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		raw = r.FormValue("url")
	default:
		var b strings.Builder
		buf := make([]byte, 1024)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		raw = b.String()
	}
	if raw == "" {
		raw = r.URL.Query().Get("url")
	}
	link, ok := saveURL(raw)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not a web address")
		return
	}
	_, title, problem, _, err := s.savePage(r.Context(), uid, link)
	if err != nil {
		serverError(w, err)
		return
	}
	slog.Info("page saved", "user", uid, "url", link)
	out := map[string]any{"saved": true, "title": title}
	if problem != "" {
		out["problem"] = problem
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteSaved removes a page from "Read later".
func (s *Server) handleDeleteSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSaved(userID(r), id); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
