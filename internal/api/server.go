// Package api exposes the JSON API, the image proxy and the static PWA.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/trapassati/ebi-furai/internal/adblock"
	"github.com/trapassati/ebi-furai/internal/auth"
	"github.com/trapassati/ebi-furai/internal/backup"
	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/feeds"
	"github.com/trapassati/ebi-furai/internal/i18n"
	"github.com/trapassati/ebi-furai/internal/push"
	"github.com/trapassati/ebi-furai/internal/store"
	"github.com/trapassati/ebi-furai/internal/update"
)

const (
	cookieName  = "rd_session"
	sessionTTL  = 60 * 24 * time.Hour
	touchEvery  = time.Hour
	maxJSONBody = 1 << 20
	maxOPMLBody = 4 << 20
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	fetcher *feeds.Fetcher
	limiter *auth.LoginLimiter
	setup   setupState

	restart    func()     // restarts the server (set by main)
	restoring  sync.Mutex // one restore at a time
	images     *content.ImageSigner
	static     *Static
	push       *push.Service
	ads        *adblock.Manager
	adOn       *atomic.Bool
	backups    *backup.Manager // nil when BACKUPS=false
	undo       undoStore
	tickets    ticketStore     // logins waiting for the two-step code
	apiTouched apiTouch        // when app passwords were last marked as used
	updates    *update.Checker // nil when UPDATE_CHECK=false
}

func New(cfg *config.Config, st *store.Store, f *feeds.Fetcher, images *content.ImageSigner, static *Static, ps *push.Service,
	ads *adblock.Manager, adOn *atomic.Bool) *Server {
	return &Server{cfg: cfg, store: st, fetcher: f, limiter: auth.NewLoginLimiter(), images: images, static: static, push: ps,
		ads: ads, adOn: adOn}
}

type ctxKey int

const (
	ctxSession ctxKey = iota
	ctxToken
)

func sessionFrom(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxSession).(*store.Session)
	return s
}

func userID(r *http.Request) int64 { return sessionFrom(r).User.ID }

// Routes builds the HTTP handler.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	// The Google Reader API, for apps like Reeder and NetNewsWire.
	r.Group(func(r chi.Router) {
		r.Use(noStore)
		s.greaderRoutes(r)
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(noStore, withLang)
		r.Route("/greader.php", s.greaderRoutes) // where apps expecting FreshRSS look
		r.Get("/setup", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)
		r.Post("/login", s.handleLogin)
		r.Post("/login/totp", s.handleLoginTOTP)
		r.Post("/save", s.handleSaveWithKey) // Shortcuts and other apps, with an app password

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)
			r.Post("/logout", s.handleLogout)
			r.Put("/me/password", s.handleChangePassword)
			r.Put("/me/refresh", s.handleSetRefresh)
			r.Put("/me/dedup", s.handleSetDedup)
			r.Post("/me/totp/setup", s.handleTOTPSetup)
			r.Post("/me/totp/enable", s.handleTOTPEnable)
			r.Post("/me/totp/disable", s.handleTOTPDisable)
			r.Post("/me/totp/recovery", s.handleRecoveryCodes)
			r.Get("/me/sessions", s.handleSessions)
			r.Delete("/me/sessions/{id}", s.handleDeleteSession)
			r.Post("/me/sessions/logout-others", s.handleLogoutOthers)
			r.Get("/me/app-passwords", s.handleAppPasswords)
			r.Post("/me/app-passwords", s.handleCreateAppPassword)
			r.Delete("/me/app-passwords/{id}", s.handleDeleteAppPassword)
			r.Put("/me/prefs", s.handlePrefs)
			r.Get("/about", s.handleAbout)

			r.Get("/sources", s.handleSources)

			r.Post("/folders", s.handleCreateFolder)
			r.Patch("/folders/{id}", s.handleUpdateFolder)
			r.Delete("/folders/{id}", s.handleDeleteFolder)

			r.Post("/feeds", s.handleAddFeed)
			r.Put("/feeds/{id}/auth", s.handleFeedAuth)
			r.Put("/feeds/{id}/url", s.handleFeedURL)
			r.Patch("/feeds/{id}", s.handleUpdateFeed)
			r.Delete("/feeds/{id}", s.handleDeleteFeed)
			r.Post("/feeds/{id}/refresh", s.handleRefreshFeed)
			r.Get("/feeds/{id}/icon", s.handleFeedIcon)

			r.Get("/filters", s.handleFilters)
			r.Post("/filters", s.handleCreateFilter)
			r.Post("/filters/preview", s.handlePreviewFilter)
			r.Delete("/filters/{id}", s.handleDeleteFilter)

			r.Get("/articles", s.handleListArticles)
			r.Post("/articles/mark-read", s.handleMarkAllRead)
			r.Post("/articles/mark-range", s.handleMarkRange)
			r.Post("/articles/mark-read/undo", s.handleUndoMarkAll)
			r.Get("/articles/{id}", s.handleGetArticle)
			r.Patch("/articles/{id}", s.handleUpdateArticle)
			r.Post("/articles/{id}/fulltext", s.handleFullText)
			r.Post("/saved", s.handleSavePage)
			r.Delete("/saved/{id}", s.handleDeleteSaved)

			r.Get("/push", s.handlePushInfo)
			r.Put("/push/settings", s.handlePushSettings)
			r.Post("/push/subscriptions", s.handlePushSubscribe)
			r.Post("/push/unsubscribe", s.handlePushUnsubscribe)
			r.Delete("/push/subscriptions/{id}", s.handlePushDeleteDevice)
			r.Post("/push/test", s.handlePushTest)

			r.Post("/offline/sync", s.handleOfflineSync)
			r.Post("/actions", s.handleActions)

			r.Get("/adfilter", s.handleAdFilterStatus)

			r.Post("/refresh", s.handleRefreshAll)
			r.Get("/opml", s.handleExportOPML)
			r.Post("/opml", s.handleImportOPML)

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Post("/users/{id}/password", s.handleResetPassword)
				r.Post("/users/{id}/disable", s.handleSetDisabled(true))
				r.Post("/users/{id}/enable", s.handleSetDisabled(false))
				r.Post("/users/{id}/totp/disable", s.handleAdminDisableTOTP)
				r.Delete("/users/{id}", s.handleDeleteUser)
				r.Put("/adfilter", s.handleAdFilterSet)
				r.Post("/adfilter/update", s.handleAdFilterUpdate)
				r.Get("/update", s.handleUpdate)
				r.Get("/backups", s.handleListBackups)
				r.Post("/backups", s.handleCreateBackup)
				r.Get("/backups/{name}", s.handleDownloadBackup)
				r.Post("/backups/{name}/restore", s.handleRestoreBackup)
				r.Post("/backups/restore", s.handleRestoreBackup)
				r.Delete("/backups/{name}", s.handleDeleteBackup)
				r.Put("/backups/webdav", s.handleSaveWebDAV)
				r.Delete("/backups/webdav", s.handleDeleteWebDAV)
				r.Post("/backups/webdav/test", s.handleTestWebDAV)
				r.Post("/backups/webdav/sync", s.handleSyncWebDAV)
			})
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "no such endpoint")
		})
	})

	r.With(s.requireAuth).Get("/img", s.handleImage)

	s.static.Mount(r)
	return r
}

// ---- middleware ----

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requireAuth loads the session from the cookie, renews it periodically
// and enforces the CSRF token on state-changing requests.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "sign-in required")
			return
		}
		hash := auth.HashToken(c.Value)
		sess, err := s.store.SessionByToken(hash)
		if errors.Is(err, store.ErrNotFound) {
			s.clearCookie(w, r)
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		if err != nil {
			// A database error (busy, I/O) is not a logout: keep the cookie.
			slog.Error("session lookup failed", "err", err)
			writeError(w, http.StatusServiceUnavailable, "the server is busy, try again")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			tok := r.Header.Get("X-CSRF-Token")
			if subtle.ConstantTimeCompare([]byte(tok), []byte(sess.CSRF)) != 1 {
				writeError(w, http.StatusForbidden, "invalid CSRF token, reload the app")
				return
			}
		}
		if time.Since(time.Unix(sess.LastSeen, 0)) > touchEvery {
			if err := s.store.TouchSession(hash, s.clientIP(r), sessionTTL); err == nil {
				s.setCookie(w, r, c.Value)
			}
		}
		// Notifications follow the language of the app last used.
		if v := r.Header.Get("X-Lang"); v != "" {
			if lang := i18n.Normalize(v); lang != sess.User.Lang {
				if s.store.SetUserLang(sess.User.ID, lang) == nil {
					sess.User.Lang = lang
				}
			}
		}
		ctx := context.WithValue(r.Context(), ctxSession, sess)
		ctx = context.WithValue(ctx, ctxToken, hash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sessionFrom(r).User.IsAdmin {
			writeError(w, http.StatusForbidden, "for administrators only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secure tells whether cookies get the Secure flag: always, never, or (auto)
// when the request came over https, directly or through a proxy or tunnel.
func (s *Server) secure(r *http.Request) bool {
	switch s.cfg.CookieSecure {
	case "true":
		return true
	case "false":
		return false
	}
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
}

// clientIP returns the real client address. CF-Connecting-IP is trusted
// only when enabled: the port must be bound to localhost behind the tunnel.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustCFHeader {
		if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- helpers ----

// langWriter carries the language of the request to writeJSON, which
// translates the messages meant for people (errors, feed errors).
type langWriter struct {
	http.ResponseWriter
	lang string
}

func (lw *langWriter) Unwrap() http.ResponseWriter { return lw.ResponseWriter }

// withLang picks the language of each API request (X-Lang from the app,
// otherwise Accept-Language).
func withLang(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := i18n.FromRequest(r)
		next.ServeHTTP(&langWriter{w, lang}, r.WithContext(i18n.WithLang(r.Context(), lang)))
	})
}

func localize(w http.ResponseWriter, v any) any {
	lw, ok := w.(*langWriter)
	if !ok || lw.lang == i18n.English {
		return v
	}
	feed := func(f *store.FeedInfo) *store.FeedInfo {
		if f == nil || f.LastError == "" {
			return f
		}
		c := *f
		c.LastError = i18n.T(lw.lang, f.LastError)
		return &c
	}
	switch x := v.(type) {
	case map[string]string:
		if e, ok := x["error"]; ok {
			x["error"] = i18n.T(lw.lang, e)
		}
	case map[string]any:
		for _, k := range []string{"error", "problem"} {
			if e, ok := x[k].(string); ok {
				x[k] = i18n.T(lw.lang, e)
			}
		}
		if f, ok := x["feed"].(*store.FeedInfo); ok {
			x["feed"] = feed(f)
		}
	case *store.Sources:
		c := *x
		c.Feeds = make([]*store.FeedInfo, len(x.Feeds))
		for i, f := range x.Feeds {
			c.Feeds[i] = feed(f)
		}
		c.Folders = make([]*store.FolderInfo, len(x.Folders))
		for i, fo := range x.Folders {
			fc := *fo
			fc.Feeds = make([]*store.FeedInfo, len(fo.Feeds))
			for j, f := range fo.Feeds {
				fc.Feeds[j] = feed(f)
			}
			c.Folders[i] = &fc
		}
		return &c
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	v = localize(w, v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// storeError maps common store errors to HTTP responses.
func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrFeedExists), errors.Is(err, store.ErrFolderExists), errors.Is(err, store.ErrUserExists),
		errors.Is(err, store.ErrTooManyFilters):
		writeError(w, http.StatusConflict, err.Error())
	default:
		serverError(w, err)
	}
}

// readJSON decodes a JSON body. Requiring application/json also blocks
// cross-site form posts (they cannot set this type without CORS).
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSONBody))
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// todayStart returns local midnight as a Unix timestamp.
func (s *Server) todayStart() int64 {
	now := time.Now().In(s.cfg.Location)
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, s.cfg.Location).Unix()
}
