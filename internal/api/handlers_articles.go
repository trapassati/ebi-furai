package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/dedup"
	"github.com/trapassati/ebi-furai/internal/netsafe"
	"github.com/trapassati/ebi-furai/internal/store"
)

func (s *Server) selection(r *http.Request, source, search string) (store.Selection, error) {
	kind, id, err := store.ParseSource(source)
	if err != nil {
		return store.Selection{}, err
	}
	return store.Selection{UserID: userID(r), Kind: kind, ID: id, TodayStart: s.todayStart(), Search: strings.TrimSpace(search)}, nil
}

func (s *Server) decorate(a *store.ArticleSummary) {
	a.Thumbnail = s.images.URL(a.Thumbnail)
}

func (s *Server) handleListArticles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sel, err := s.selection(r, q.Get("source"), q.Get("q"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	opt := store.ListOptions{
		UnreadOnly:  q.Get("filter") == "unread",
		StarredOnly: q.Get("filter") == "starred",
		Oldest:      q.Get("order") == "old",
		Cursor:      q.Get("cursor"),
		Limit:       limit,
	}
	maxID := s.store.MaxArticleID(sel.UserID)
	items, next, err := s.store.ListArticles(sel, opt)
	if errors.Is(err, store.ErrBadCursor) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		// Search terms are quoted for FTS5, so this is the database itself.
		serverError(w, err)
		return
	}
	for _, a := range items {
		s.decorate(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "max_id": maxID})
}

func (s *Server) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
		MaxID  int64  `json:"max_id"`
		Search string `json:"q"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	sel, err := s.selection(r, req.Source, req.Search)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.MaxID <= 0 {
		req.MaxID = s.store.MaxArticleID(sel.UserID)
	}
	ids, err := s.store.MarkAllRead(sel, req.MaxID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := map[string]any{"marked": len(ids)}
	if len(ids) > 0 {
		out["undo"] = s.undo.put(sel.UserID, ids)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMarkRange marks as read the articles of a list above or below
// one ("Segna come letti quelli sopra / sotto"), in the order and with the
// filter the list is shown with.
func (s *Server) handleMarkRange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source    string `json:"source"`
		Search    string `json:"q"`
		Filter    string `json:"filter"` // "starred" limits to the starred ones
		Order     string `json:"order"`  // "old" = oldest first
		ID        int64  `json:"id"`     // the article the menu was opened on
		Direction string `json:"direction"`
		MaxID     int64  `json:"max_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	sel, err := s.selection(r, req.Source, req.Search)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Direction != "above" && req.Direction != "below" {
		writeError(w, http.StatusBadRequest, "invalid direction")
		return
	}
	pivot, err := s.store.GetArticle(sel.UserID, req.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	if req.MaxID <= 0 {
		req.MaxID = s.store.MaxArticleID(sel.UserID)
	}
	opt := store.ListOptions{StarredOnly: req.Filter == "starred", Oldest: req.Order == "old"}
	ids, err := s.store.MarkRange(sel, opt, pivot.PublishedAt, pivot.ID, req.Direction == "above", req.MaxID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := map[string]any{"marked": len(ids)}
	if len(ids) > 0 {
		out["undo"] = s.undo.put(sel.UserID, ids)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUndoMarkAll brings back as unread what a "mark all as read" changed,
// within a couple of minutes of it.
func (s *Server) handleUndoMarkAll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ids, ok := s.undo.take(userID(r), req.Token)
	if !ok {
		writeError(w, http.StatusGone, "it can no longer be undone")
		return
	}
	n, err := s.store.MarkUnread(userID(r), ids)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"restored": n})
}

func (s *Server) articleJSON(a *store.Article) map[string]any {
	s.decorate(&a.ArticleSummary)
	return map[string]any{
		"id": a.ID, "feed_id": a.FeedID, "feed_title": a.FeedTitle, "feed_has_icon": a.FeedHasIcon,
		"title": a.Title, "url": a.URL, "author": a.Author, "published_at": a.PublishedAt, "preview": a.Preview,
		"read": a.Read, "starred": a.Starred, "site_url": a.SiteURL, "thumbnail": a.Thumbnail,
		"content": s.images.Rewrite(a.Content), "has_full": a.HasFull, "words": a.Words,
		"full_content": s.images.Rewrite(a.FullContent),
	}
}

func (s *Server) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid := userID(r)
	a, err := s.store.GetArticle(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	// The app marks an article as read when it is opened (and its copies
	// from other feeds with it).
	var also []int64
	if r.URL.Query().Get("mark_read") == "1" && !a.Read {
		if err := s.store.SetRead(uid, id, true); err == nil {
			a.Read = true
			also, _ = s.store.ReadDuplicates(uid, id)
		}
	}
	out := s.articleJSON(a)
	if len(also) > 0 {
		out["also_read"] = also
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateArticle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Read    *bool `json:"read"`
		Starred *bool `json:"starred"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	uid := userID(r)
	out := map[string]any{"ok": true}
	if req.Read != nil {
		if err := s.store.SetRead(uid, id, *req.Read); err != nil {
			storeError(w, err)
			return
		}
		if *req.Read {
			if also, _ := s.store.ReadDuplicates(uid, id); len(also) > 0 {
				out["also_read"] = also
			}
		}
	}
	if req.Starred != nil {
		if err := s.store.SetStarred(uid, id, *req.Starred); err != nil {
			storeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFullText(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid := userID(r)
	a, err := s.store.GetArticle(uid, id)
	if err != nil {
		storeError(w, err)
		return
	}
	if a.FullContent == "" {
		if a.URL == "" {
			writeError(w, http.StatusUnprocessableEntity, "the article has no link")
			return
		}
		html, err := content.FetchFullText(r.Context(), s.fetcher.Client(), a.URL)
		if err != nil {
			msg := err.Error()
			if errors.Is(err, netsafe.ErrBlocked) {
				msg = "address not allowed"
			} else if errors.Is(err, context.DeadlineExceeded) {
				msg = "the site did not respond in time"
			}
			writeError(w, http.StatusBadGateway, "full text not available: "+msg)
			return
		}
		words := dedup.Words(content.PlainText(html))
		if err := s.store.SetFullContent(uid, id, html, words); err != nil {
			storeError(w, err)
			return
		}
		a.FullContent = html
		a.Words = max(a.Words, words)
	}
	writeJSON(w, http.StatusOK, map[string]any{"full_content": s.images.Rewrite(a.FullContent), "words": a.Words})
}

// ---- image proxy ----

const maxImageSize = 15 << 20

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if !s.images.Enabled || !s.images.Verify(raw, r.URL.Query().Get("s")) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	resp, err := netsafe.Get(ctx, s.fetcher.Client(), raw, map[string]string{
		"Accept": "image/avif,image/webp,image/apng,image/*;q=0.8",
	})
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "image/") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	if resp.ContentLength > maxImageSize {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	if resp.ContentLength > 0 {
		h.Set("Content-Length", fmt.Sprint(resp.ContentLength))
	}
	// A proxied SVG opened directly must not run scripts on our origin.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=604800, immutable")
	io.Copy(w, io.LimitReader(resp.Body, maxImageSize))
}
