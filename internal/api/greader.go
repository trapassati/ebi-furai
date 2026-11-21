package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/trapassati/ebi-furai/internal/auth"
	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/feeds"
	"github.com/trapassati/ebi-furai/internal/store"
)

// The Google Reader API, as FreshRSS and Miniflux offer it: apps like
// Reeder, NetNewsWire, ReadKit and FeedMe sync with it. It is served at the
// root of the app (/accounts/ClientLogin, /reader/api/0/…) and, for apps
// that expect FreshRSS, under /api/greader.php too.
//
// Apps sign in with the username and an app password (ClientLogin), then
// send "Authorization: GoogleLogin auth=<token>". A header is never sent by
// a browser on its own, so these requests cannot be forged by another site
// and need no CSRF token; the "T" token apps ask for is accepted as is.

const (
	grItemPrefix  = "tag:google.com,2005:reader/item/"
	grReadingList = "user/-/state/com.google/reading-list"
	grRead        = "user/-/state/com.google/read"
	grStarred     = "user/-/state/com.google/starred"
	grKeptUnread  = "user/-/state/com.google/kept-unread"
	grLabel       = "user/-/label/"
	grMaxBody     = 4 << 20
	grMaxIDs      = 10000
	grMaxContents = 1000
)

type grCtxKey struct{}

type grUser struct {
	*store.User
	passwordID int64
}

func grUserFrom(r *http.Request) *grUser { return r.Context().Value(grCtxKey{}).(*grUser) }

// apiTouch limits the "last used" updates of app passwords to one an hour.
type apiTouch struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

func (t *apiTouch) due(id int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[int64]time.Time{}
	}
	if time.Since(t.last[id]) < time.Hour {
		return false
	}
	t.last[id] = time.Now()
	return true
}

// greaderRoutes adds the API to r (at the root, or under a prefix).
func (s *Server) greaderRoutes(r chi.Router) {
	r.Post("/accounts/ClientLogin", s.grClientLogin)
	r.Get("/accounts/ClientLogin", s.grClientLogin)
	r.Route("/reader/api/0", func(r chi.Router) {
		r.Use(s.grAuth)
		r.Get("/token", s.grToken)
		r.Get("/user-info", s.grUserInfo)
		r.Get("/subscription/list", s.grSubscriptions)
		r.Post("/subscription/edit", s.grEditSubscription)
		r.Post("/subscription/quickadd", s.grQuickAdd)
		r.Get("/tag/list", s.grTags)
		r.Post("/rename-tag", s.grRenameTag)
		r.Post("/disable-tag", s.grDisableTag)
		r.Get("/unread-count", s.grUnreadCount)
		r.Get("/stream/items/ids", s.grItemIDs)
		r.Get("/stream/items/contents", s.grItemContents)
		r.Post("/stream/items/contents", s.grItemContents)
		r.Get("/stream/contents", s.grStreamContents)
		r.Get("/stream/contents/*", s.grStreamContents)
		r.Post("/edit-tag", s.grEditTag)
		r.Post("/mark-all-as-read", s.grMarkAllRead)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { grText(w, http.StatusNotFound, "Not found") })
	})
}

// ---- responses ----

func grText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func grOK(w http.ResponseWriter) { grText(w, http.StatusOK, "OK") }

// grJSON writes JSON without writeJSON's translation of "error" fields.
func grJSON(w http.ResponseWriter, v any) {
	if lw, ok := w.(*langWriter); ok {
		w = lw.ResponseWriter
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func grForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, grMaxBody)
	if err := r.ParseForm(); err != nil {
		grText(w, http.StatusBadRequest, "Bad request")
		return false
	}
	return true
}

// ---- sign-in ----

func (s *Server) grClientLogin(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	user := strings.TrimSpace(r.Form.Get("Email"))
	pass := r.Form.Get("Passwd")
	ip := s.clientIP(r)
	if !s.limiter.Allowed(ip, user) {
		grText(w, http.StatusTooManyRequests, "Error=TooManyAttempts\n")
		return
	}
	uid, pid, err := s.store.AppPasswordLogin(user, appPasswordHash(pass))
	if errors.Is(err, store.ErrNotFound) {
		s.limiter.Fail(ip, user)
		grText(w, http.StatusUnauthorized, "Error=BadAuthentication\n")
		return
	}
	if err != nil {
		slog.Error("api sign-in", "err", err)
		grText(w, http.StatusInternalServerError, "Error=Unknown\n")
		return
	}
	s.limiter.Success(ip)
	token := strconv.FormatInt(uid, 10) + "/" + auth.RandomToken(32)
	if err := s.store.CreateAPIToken(auth.HashToken(token), pid); err != nil {
		slog.Error("api token", "err", err)
		grText(w, http.StatusInternalServerError, "Error=Unknown\n")
		return
	}
	s.store.TouchAppPassword(pid, ip)
	slog.Info("app signed in", "user", user, "ip", ip)
	if r.Form.Get("output") == "json" {
		grJSON(w, map[string]string{"SID": token, "LSID": token, "Auth": token})
		return
	}
	grText(w, http.StatusOK, "SID="+token+"\nLSID="+token+"\nAuth="+token+"\n")
}

func (s *Server) grAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if h := r.Header.Get("Authorization"); h != "" {
			if v, ok := strings.CutPrefix(h, "GoogleLogin auth="); ok {
				token = strings.TrimSpace(v)
			}
		}
		if token == "" {
			grText(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		u, pid, err := s.store.APIUser(auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			grText(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		if err != nil {
			slog.Error("api token lookup", "err", err)
			grText(w, http.StatusServiceUnavailable, "Busy")
			return
		}
		if s.apiTouched.due(pid) {
			s.store.TouchAppPassword(pid, s.clientIP(r))
		}
		ctx := context.WithValue(r.Context(), grCtxKey{}, &grUser{User: u, passwordID: pid})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// grToken returns the "T" token apps send back with every change. Changes
// are protected by the Authorization header already (see above).
func (s *Server) grToken(w http.ResponseWriter, r *http.Request) {
	grText(w, http.StatusOK, auth.HashToken("t:" + strconv.FormatInt(grUserFrom(r).passwordID, 10))[:57]+"\n")
}

func (s *Server) grUserInfo(w http.ResponseWriter, r *http.Request) {
	u := grUserFrom(r)
	id := strconv.FormatInt(u.ID, 10)
	grJSON(w, map[string]any{"userId": id, "userName": u.Username, "userProfileId": id, "userEmail": "",
		"isBloggerUser": false, "signupTimeSec": 0, "isMultiLoginEnabled": false})
}

// ---- subscriptions and folders ----

func grLabelID(name string) string { return grLabel + name }

func (s *Server) grSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.APIFeeds(grUserFrom(r).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	subs := make([]map[string]any, 0, len(list))
	for _, f := range list {
		cats := []map[string]string{}
		if f.Folder != "" {
			cats = append(cats, map[string]string{"id": grLabelID(f.Folder), "label": f.Folder})
		}
		subs = append(subs, map[string]any{
			"id": "feed/" + strconv.FormatInt(f.ID, 10), "title": f.Title, "categories": cats,
			"url": f.URL, "htmlUrl": f.SiteURL, "iconUrl": "", "sortid": fmt.Sprintf("%08X", f.ID), "firstitemmsec": "0",
		})
	}
	grJSON(w, map[string]any{"subscriptions": subs})
}

func (s *Server) grTags(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.APIFolders(grUserFrom(r).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	tags := []map[string]string{{"id": grStarred}}
	for _, n := range names {
		tags = append(tags, map[string]string{"id": grLabelID(n), "type": "folder"})
	}
	grJSON(w, map[string]any{"tags": tags})
}

// normStream turns "user/1234/…" into "user/-/…".
func normStream(id string) string {
	id = strings.TrimSpace(id)
	if rest, ok := strings.CutPrefix(id, "user/"); ok {
		if i := strings.IndexByte(rest, '/'); i > 0 {
			return "user/-/" + rest[i+1:]
		}
	}
	return id
}

// grFeedID resolves "feed/12" or "feed/https://…" to a feed of the user.
func (s *Server) grFeedID(uid int64, stream string) (int64, error) {
	ref, ok := strings.CutPrefix(stream, "feed/")
	if !ok {
		return 0, store.ErrNotFound
	}
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		if _, err := s.store.FeedFolderID(uid, id); err == nil {
			return id, nil
		}
	}
	if id, err := s.store.FeedIDByURL(uid, ref); err == nil {
		return id, nil
	}
	return s.store.FeedIDByURL(uid, feeds.NormalizeURL(ref))
}

// grSubscribe adds a feed (finding it from a page address if needed) and
// fetches it at once, like the app does. An existing feed is returned.
func (s *Server) grSubscribe(ctx context.Context, uid int64, raw string, folderID int64) (int64, error) {
	cands, err := s.fetcher.Discover(ctx, raw, nil)
	if err != nil {
		return 0, err
	}
	c := cands[0]
	if id, err := s.store.FeedIDByURL(uid, c.URL); err == nil {
		return id, nil
	}
	id, err := s.store.CreateFeed(uid, c.URL, c.Title, "", folderID)
	if err != nil {
		return 0, err
	}
	if ff, err := s.store.FetchFeedByID(uid, id); err == nil {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
		s.fetcher.FetchNow(fctx, []*store.FetchFeed{ff})
		cancel()
	}
	return id, nil
}

func (s *Server) grFolder(uid int64, label string, create bool) (int64, error) {
	name, ok := strings.CutPrefix(normStream(label), grLabel)
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return 0, store.ErrNotFound
	}
	if create {
		return s.store.FolderByName(uid, name)
	}
	return s.store.FolderIDByName(uid, name)
}

func (s *Server) grEditSubscription(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	uid := grUserFrom(r).ID
	ac := r.Form.Get("ac")
	title := strings.TrimSpace(r.Form.Get("t"))
	add, remove := r.Form.Get("a"), r.Form.Get("r")
	for _, stream := range r.Form["s"] {
		stream = strings.TrimSpace(stream)
		switch ac {
		case "subscribe":
			ref, ok := strings.CutPrefix(stream, "feed/")
			if !ok {
				grText(w, http.StatusBadRequest, "Bad stream")
				return
			}
			var folder int64
			if add != "" {
				folder, _ = s.grFolder(uid, add, true)
			}
			id, err := s.grSubscribe(r.Context(), uid, ref, folder)
			if err != nil {
				grText(w, http.StatusBadRequest, discoverMessage(err))
				return
			}
			if title != "" {
				s.store.RenameFeed(uid, id, title)
			}
		case "unsubscribe":
			id, err := s.grFeedID(uid, stream)
			if err == nil {
				err = s.store.DeleteFeed(uid, id)
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				serverError(w, err)
				return
			}
		case "edit":
			id, err := s.grFeedID(uid, stream)
			if err != nil {
				grText(w, http.StatusNotFound, "No such feed")
				return
			}
			if title != "" {
				s.store.RenameFeed(uid, id, title)
			}
			if remove != "" {
				cur, _ := s.store.FeedFolderID(uid, id)
				if f, err := s.grFolder(uid, remove, false); err == nil && f == cur {
					s.store.MoveFeed(uid, id, 0)
				}
			}
			if add != "" {
				if f, err := s.grFolder(uid, add, true); err == nil {
					s.store.MoveFeed(uid, id, f)
				}
			}
		default:
			grText(w, http.StatusBadRequest, "Bad action")
			return
		}
	}
	grOK(w)
}

func (s *Server) grQuickAdd(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	uid := grUserFrom(r).ID
	raw := strings.TrimSpace(r.Form.Get("quickadd"))
	raw = strings.TrimPrefix(raw, "feed/")
	id, err := s.grSubscribe(r.Context(), uid, raw, 0)
	if err != nil {
		grJSON(w, map[string]any{"numResults": 0, "query": raw, "error": discoverMessage(err)})
		return
	}
	title := ""
	if info, err := s.store.FeedInfo(uid, id); err == nil {
		title = info.Title
	}
	grJSON(w, map[string]any{"numResults": 1, "query": raw, "streamId": "feed/" + strconv.FormatInt(id, 10), "streamName": title})
}

func (s *Server) grRenameTag(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	uid := grUserFrom(r).ID
	id, err := s.grFolder(uid, r.Form.Get("s"), false)
	if err != nil {
		grText(w, http.StatusNotFound, "No such folder")
		return
	}
	name, ok := strings.CutPrefix(normStream(r.Form.Get("dest")), grLabel)
	if name = strings.TrimSpace(name); !ok || name == "" {
		grText(w, http.StatusBadRequest, "Bad name")
		return
	}
	if err := s.store.RenameFolder(uid, id, name); err != nil {
		if errors.Is(err, store.ErrFolderExists) {
			grText(w, http.StatusConflict, err.Error())
			return
		}
		serverError(w, err)
		return
	}
	grOK(w)
}

func (s *Server) grDisableTag(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	uid := grUserFrom(r).ID
	for _, label := range r.Form["s"] {
		if id, err := s.grFolder(uid, label, false); err == nil {
			s.store.DeleteFolder(uid, id) // its feeds stay, outside any folder
		}
	}
	grOK(w)
}

// ---- articles ----

func usec(sec int64) string { return strconv.FormatInt(sec*1_000_000, 10) }

func (s *Server) grUnreadCount(w http.ResponseWriter, r *http.Request) {
	uid := grUserFrom(r).ID
	counts, err := s.store.APIUnreadCounts(uid)
	if err != nil {
		serverError(w, err)
		return
	}
	feedList, err := s.store.APIFeeds(uid)
	if err != nil {
		serverError(w, err)
		return
	}
	folderOf := map[int64]string{}
	for _, f := range feedList {
		folderOf[f.ID] = f.Folder
	}
	type agg struct{ n, newest int64 }
	folders := map[string]*agg{}
	var total agg
	out := []map[string]any{}
	for _, c := range counts {
		out = append(out, map[string]any{"id": "feed/" + strconv.FormatInt(c.FeedID, 10), "count": c.Count,
			"newestItemTimestampUsec": usec(c.Newest)})
		total.n += int64(c.Count)
		total.newest = max(total.newest, c.Newest)
		if name := folderOf[c.FeedID]; name != "" {
			if folders[name] == nil {
				folders[name] = &agg{}
			}
			folders[name].n += int64(c.Count)
			folders[name].newest = max(folders[name].newest, c.Newest)
		}
	}
	for name, a := range folders {
		out = append(out, map[string]any{"id": grLabelID(name), "count": a.n, "newestItemTimestampUsec": usec(a.newest)})
	}
	out = append(out, map[string]any{"id": grReadingList, "count": total.n, "newestItemTimestampUsec": usec(total.newest)})
	grJSON(w, map[string]any{"max": max(total.n, 1000), "unreadcounts": out})
}

// grStream reads the stream and its filters from the query: s (or the
// path), xt / it (exclude / include a state), ot / nt (newer / older than,
// in seconds). ok is false for a stream that has nothing (a folder that
// does not exist, or one this server does not keep).
func (s *Server) grStream(r *http.Request, stream string) (st store.APIStream, ok bool) {
	uid := grUserFrom(r).ID
	q := r.Form
	st.UserID = uid
	stream = normStream(stream)
	switch {
	case stream == "" || stream == grReadingList:
	case stream == grStarred:
		st.Starred = true
	case stream == grRead:
		st.Read = true
	case stream == grKeptUnread:
		st.Unread = true
	case strings.HasPrefix(stream, "feed/"):
		id, err := s.grFeedID(uid, stream)
		if err != nil {
			return st, false
		}
		st.FeedID = id
	case strings.HasPrefix(stream, grLabel):
		id, err := s.grFolder(uid, stream, false)
		if err != nil {
			return st, false
		}
		st.FolderID = id
	default:
		return st, false
	}
	for _, x := range q["xt"] {
		switch normStream(x) {
		case grRead:
			st.Unread = true
		case grStarred:
			st.NoStar = true
		}
	}
	for _, x := range q["it"] {
		switch normStream(x) {
		case grRead:
			st.Read = true
		case grStarred:
			st.Starred = true
		}
	}
	if st.Read && st.Unread {
		return st, false
	}
	if v, err := strconv.ParseInt(q.Get("ot"), 10, 64); err == nil && v > 0 {
		st.Since = v
	}
	if v, err := strconv.ParseInt(q.Get("nt"), 10, 64); err == nil && v > 0 {
		st.Until = v
	}
	return st, true
}

func grCount(r *http.Request, def, most int) int {
	n, err := strconv.Atoi(r.Form.Get("n"))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, most)
}

func (s *Server) grItemIDs(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	st, ok := s.grStream(r, r.Form.Get("s"))
	refs := []map[string]any{}
	next := ""
	if ok {
		list, cont, err := s.store.APIItemIDs(st, r.Form.Get("r") == "o", r.Form.Get("c"), grCount(r, 20, grMaxIDs))
		if errors.Is(err, store.ErrBadCursor) {
			grText(w, http.StatusBadRequest, "Bad continuation")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		for _, it := range list {
			refs = append(refs, map[string]any{"id": strconv.FormatInt(it.ID, 10), "directStreamIds": []string{},
				"timestampUsec": usec(it.PublishedAt)})
		}
		next = cont
	}
	out := map[string]any{"itemRefs": refs}
	if next != "" {
		out["continuation"] = next
	}
	grJSON(w, out)
}

// parseItemID reads an item id in any of the forms apps send: the long
// "tag:google.com,2005:reader/item/<hex>", decimal, or bare hex.
func parseItemID(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	base := 10
	if rest, ok := strings.CutPrefix(s, grItemPrefix); ok {
		s, base = rest, 16
	} else if strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		base = 16
	}
	v, err := strconv.ParseUint(s, base, 64)
	if err != nil || v == 0 || v > 1<<62 {
		return 0, false
	}
	return int64(v), true
}

func grItemIDsFrom(r *http.Request) []int64 {
	ids := []int64{}
	for _, v := range r.Form["i"] {
		if id, ok := parseItemID(v); ok {
			ids = append(ids, id)
		}
		if len(ids) >= grMaxIDs {
			break
		}
	}
	return ids
}

// apiImages leaves images at their own address: apps load them directly
// (the image proxy needs the app's session cookie).
var apiImages = content.NewImageSigner(nil, false)

func grItemJSON(it *store.APIItem) map[string]any {
	cats := []string{grReadingList}
	if it.Folder != "" {
		cats = append(cats, grLabelID(it.Folder))
	}
	if it.Read {
		cats = append(cats, grRead)
	}
	if it.Starred {
		cats = append(cats, grStarred)
	}
	feed := "feed/" + strconv.FormatInt(it.FeedID, 10)
	links := []map[string]string{}
	if it.URL != "" {
		links = append(links, map[string]string{"href": it.URL, "type": "text/html"})
	}
	return map[string]any{
		"id":            fmt.Sprintf("%s%016x", grItemPrefix, it.ID),
		"crawlTimeMsec": strconv.FormatInt(it.FetchedAt*1000, 10),
		"timestampUsec": usec(it.PublishedAt),
		"published":     it.PublishedAt,
		"updated":       it.PublishedAt,
		"title":         it.Title,
		"author":        it.Author,
		"canonical":     links,
		"alternate":     links,
		"categories":    cats,
		"origin":        map[string]string{"streamId": feed, "title": it.FeedTitle, "htmlUrl": it.SiteURL},
		"summary":       map[string]string{"direction": "ltr", "content": apiImages.Rewrite(it.Content)},
	}
}

func (s *Server) grItemsJSON(uid int64, ids []int64) ([]map[string]any, error) {
	list, err := s.store.APIItems(uid, ids)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		out = append(out, grItemJSON(it))
	}
	return out, nil
}

func (s *Server) grItemContents(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	ids := grItemIDsFrom(r)
	if len(ids) > grMaxContents {
		ids = ids[:grMaxContents]
	}
	items, err := s.grItemsJSON(grUserFrom(r).ID, ids)
	if err != nil {
		serverError(w, err)
		return
	}
	grJSON(w, map[string]any{"id": grReadingList, "updated": time.Now().Unix(), "items": items})
}

func (s *Server) grStreamContents(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	stream := chi.URLParam(r, "*")
	if stream == "" {
		stream = r.Form.Get("s")
	}
	if stream == "" {
		stream = grReadingList
	}
	out := map[string]any{"direction": "ltr", "id": stream, "updated": time.Now().Unix(), "items": []any{}}
	st, ok := s.grStream(r, stream)
	if ok {
		refs, cont, err := s.store.APIItemIDs(st, r.Form.Get("r") == "o", r.Form.Get("c"), grCount(r, 20, grMaxContents))
		if errors.Is(err, store.ErrBadCursor) {
			grText(w, http.StatusBadRequest, "Bad continuation")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		ids := make([]int64, len(refs))
		for i, ref := range refs {
			ids[i] = ref.ID
		}
		items, err := s.grItemsJSON(st.UserID, ids)
		if err != nil {
			serverError(w, err)
			return
		}
		out["items"] = items
		if cont != "" {
			out["continuation"] = cont
		}
	}
	grJSON(w, out)
}

func (s *Server) grEditTag(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	uid := grUserFrom(r).ID
	ids := grItemIDsFrom(r)
	if len(ids) == 0 {
		grOK(w)
		return
	}
	apply := func(tag string, on bool) error {
		switch normStream(tag) {
		case grRead:
			return s.store.SetReadIDs(uid, ids, on)
		case grKeptUnread:
			if on {
				return s.store.SetReadIDs(uid, ids, false)
			}
		case grStarred:
			return s.store.SetStarredIDs(uid, ids, on)
		}
		return nil // labels on articles and other states are not kept
	}
	for _, tag := range r.Form["r"] {
		if err := apply(tag, false); err != nil {
			serverError(w, err)
			return
		}
	}
	for _, tag := range r.Form["a"] {
		if err := apply(tag, true); err != nil {
			serverError(w, err)
			return
		}
	}
	grOK(w)
}

func (s *Server) grMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if !grForm(w, r) {
		return
	}
	st, ok := s.grStream(r, r.Form.Get("s"))
	if !ok {
		grOK(w)
		return
	}
	var until int64
	if ts, err := strconv.ParseInt(r.Form.Get("ts"), 10, 64); err == nil && ts > 0 {
		// Microseconds, but some apps send seconds or nanoseconds.
		switch {
		case ts > 1e17:
			until = ts / 1e9
		case ts > 1e14:
			until = ts / 1e6
		default:
			until = ts
		}
	}
	if _, err := s.store.APIMarkAllRead(st, until); err != nil {
		serverError(w, err)
		return
	}
	grOK(w)
}
