package api

import (
	"errors"
	"net/http"

	"github.com/trapassati/ebi-furai/internal/store"
)

const (
	defaultOfflineLimit = 200
	maxOfflineLimit     = 1000
	offlineBatch        = 60 // full articles per response; the client asks again while "more" is true
	maxHave             = 3000
	maxActions          = 500
)

// handleOfflineSync tells a device which articles to keep offline. The
// device sends the ids it already has; the response carries the state of
// the whole set (ids not listed must be dropped) and the full content of
// up to offlineBatch missing articles.
func (s *Server) handleOfflineSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Have  []int64 `json:"have"`
		Limit int     `json:"limit"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Limit <= 0 {
		req.Limit = defaultOfflineLimit
	}
	req.Limit = min(req.Limit, maxOfflineLimit)
	if len(req.Have) > maxHave {
		req.Have = req.Have[:maxHave]
	}
	uid := userID(r)
	set, err := s.store.OfflineSet(uid, req.Limit)
	if err != nil {
		serverError(w, err)
		return
	}
	have := make(map[int64]bool, len(req.Have))
	for _, id := range req.Have {
		have[id] = true
	}
	states := make([][3]int64, 0, len(set))
	var missing []int64
	for _, st := range set {
		states = append(states, [3]int64{st.ID, b2i(st.Read), b2i(st.Starred)})
		if !have[st.ID] {
			missing = append(missing, st.ID)
		}
	}
	more := len(missing) > offlineBatch
	if more {
		missing = missing[:offlineBatch]
	}
	list, err := s.store.GetArticles(uid, missing)
	if err != nil {
		serverError(w, err)
		return
	}
	articles := make([]map[string]any, 0, len(list))
	for _, a := range list {
		articles = append(articles, s.articleJSON(a))
	}
	src, err := s.store.Sources(uid, s.todayStart())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"states":   states, // [id, read, starred]
		"articles": articles,
		"more":     more,
		"sources":  src,
	})
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// handleActions applies actions queued by a device while it was offline,
// in order. Articles that no longer exist are skipped.
func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actions []struct {
			Kind    string `json:"kind"` // "article" or "mark_read"
			ID      int64  `json:"id"`
			Read    *bool  `json:"read"`
			Starred *bool  `json:"starred"`
			Source  string `json:"source"`
			MaxID   int64  `json:"max_id"`
			Search  string `json:"q"`
		} `json:"actions"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Actions) > maxActions {
		writeError(w, http.StatusBadRequest, "too many actions in one request")
		return
	}
	uid := userID(r)
	applied, skipped := 0, 0
	for _, a := range req.Actions {
		var err error
		switch a.Kind {
		case "article":
			if a.Read != nil {
				err = s.store.SetRead(uid, a.ID, *a.Read)
				if err == nil && *a.Read {
					_, err = s.store.ReadDuplicates(uid, a.ID)
				}
			}
			if err == nil && a.Starred != nil {
				err = s.store.SetStarred(uid, a.ID, *a.Starred)
			}
		case "mark_read":
			sel, serr := s.selection(r, a.Source, a.Search)
			if serr != nil || a.MaxID <= 0 {
				skipped++
				continue
			}
			_, err = s.store.MarkAllRead(sel, a.MaxID)
		default:
			skipped++
			continue
		}
		switch {
		case errors.Is(err, store.ErrNotFound):
			skipped++
		case err != nil:
			serverError(w, err)
			return
		default:
			applied++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"applied": applied, "skipped": skipped})
}
