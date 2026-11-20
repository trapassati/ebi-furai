package api

import (
	"net/http"

	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/filter"
	"github.com/trapassati/ebi-furai/internal/store"
)

type filterRequest struct {
	Terms    []string `json:"terms"` // as typed: commas or new lines separate them
	Action   string   `json:"action"`
	FeedID   int64    `json:"feed_id"`
	FolderID int64    `json:"folder_id"`
	InBody   bool     `json:"in_body"`
	Existing bool     `json:"existing"` // also apply to the unread articles already there
}

// parse validates the request; it writes the error and returns nil when
// it is not valid.
func (req *filterRequest) parse(w http.ResponseWriter) *store.FilterInfo {
	terms := filter.ParseTerms(req.Terms)
	switch {
	case len(terms) == 0:
		writeError(w, http.StatusBadRequest, "write at least one word")
		return nil
	case len(terms) > filter.MaxTerms:
		writeError(w, http.StatusBadRequest, "too many words in a filter (at most 50)")
		return nil
	case req.Action != "hide" && req.Action != "read":
		writeError(w, http.StatusBadRequest, "invalid action")
		return nil
	case req.FeedID > 0 && req.FolderID > 0:
		writeError(w, http.StatusBadRequest, "choose a feed or a folder")
		return nil
	}
	return &store.FilterInfo{Terms: terms, Action: req.Action, FeedID: req.FeedID, FolderID: req.FolderID, InBody: req.InBody}
}

// matching returns the unread articles a rule catches now, newest first.
func (s *Server) matching(userID int64, rule *filter.Rule) ([]store.FilterCandidate, error) {
	list, err := s.store.FilterCandidates(userID, rule.FeedID, rule.FolderID, rule.InBody)
	if err != nil {
		return nil, err
	}
	var out []store.FilterCandidate
	for _, c := range list {
		text := ""
		if rule.InBody {
			text = content.PlainText(c.Content)
		}
		if rule.Match(c.Title, text) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Server) handleFilters(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Filters(userID(r))
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"filters": list})
}

// handlePreviewFilter tells how many unread articles a filter would catch
// now, with a few titles, while it is being written.
func (s *Server) handlePreviewFilter(w http.ResponseWriter, r *http.Request) {
	var req filterRequest
	if !readJSON(w, r, &req) {
		return
	}
	terms := filter.ParseTerms(req.Terms)
	if len(terms) == 0 || len(terms) > filter.MaxTerms {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "titles": []string{}})
		return
	}
	rule := &filter.Rule{Terms: terms, FeedID: req.FeedID, FolderID: req.FolderID, InBody: req.InBody}
	found, err := s.matching(userID(r), rule)
	if err != nil {
		serverError(w, err)
		return
	}
	titles := []string{}
	for _, c := range found[:min(3, len(found))] {
		titles = append(titles, c.Title)
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(found), "titles": titles, "terms": terms})
}

func (s *Server) handleCreateFilter(w http.ResponseWriter, r *http.Request) {
	var req filterRequest
	if !readJSON(w, r, &req) {
		return
	}
	f := req.parse(w)
	if f == nil {
		return
	}
	uid := userID(r)
	if err := s.store.CreateFilter(uid, f); err != nil {
		storeError(w, err)
		return
	}
	out := map[string]any{"filter": f, "applied": 0}
	if req.Existing {
		found, err := s.matching(uid, f.Rule())
		if err != nil {
			serverError(w, err)
			return
		}
		ids := make([]int64, len(found))
		for i, c := range found {
			ids[i] = c.ID
		}
		var changed []int64
		if f.Action == "hide" {
			changed, err = s.store.HideArticles(uid, f.ID, ids)
			f.Hidden = len(changed)
		} else {
			changed, err = s.store.MarkReadIDs(uid, ids)
			if len(changed) > 0 {
				out["undo"] = s.undo.put(uid, changed)
			}
		}
		if err != nil {
			serverError(w, err)
			return
		}
		out["applied"] = len(changed)
	}
	writeJSON(w, http.StatusCreated, out)
}

// handleDeleteFilter removes a filter: what it hid comes back as unread.
func (s *Server) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	n, err := s.store.DeleteFilter(userID(r), id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"restored": n})
}
