// Command ebi-furai is a self-hosted RSS reader served as a PWA.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata" // embedded time zones: the runtime image has none

	"github.com/trapassati/ebi-furai/internal/adblock"
	"github.com/trapassati/ebi-furai/internal/api"
	"github.com/trapassati/ebi-furai/internal/auth"
	"github.com/trapassati/ebi-furai/internal/backup"
	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/content"
	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/feeds"
	"github.com/trapassati/ebi-furai/internal/push"
	"github.com/trapassati/ebi-furai/internal/store"
	"github.com/trapassati/ebi-furai/internal/update"
	"github.com/trapassati/ebi-furai/internal/websub"
	"github.com/trapassati/ebi-furai/web"
)

// version is set by the release builds: -ldflags "-X main.version=1.2.0".
var version = "dev"

func main() {
	api.Release = version
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "", "serve":
		err = run()
		if errors.Is(err, errRestart) {
			err = restartSelf()
		}
	case "healthcheck":
		err = healthcheck()
	case "backup":
		err = runBackup(os.Args[2:])
	case "reset-2fa":
		err = reset2FA(os.Args[2:])
	default:
		err = errors.New("unknown command: use serve, healthcheck, backup [file] or reset-2fa <user>")
	}
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	// A backup restored from the app waits here for this start.
	restored, err := backup.ApplyPending(cfg.DBPath())
	if err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer conn.Close()
	st := store.New(conn)
	if restored {
		// The sessions in the backup belong to another moment.
		if err := st.DeleteAllSessions(); err != nil {
			return err
		}
		slog.Warn("database restored from a backup: everyone signs in again")
	}

	setupCode, err := bootstrapAdmin(cfg, st)
	if err != nil {
		return err
	}

	key, err := st.SecretKey("image_proxy_key")
	if err != nil {
		return err
	}
	images := content.NewImageSigner(key, cfg.ImageProxy)

	static, err := api.NewStatic(web.FS, "static", cfg.AppName, cfg.ImageProxy)
	if err != nil {
		return err
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(sigCtx)
	defer cancel()
	restart := make(chan struct{}, 1)
	// The work in the background ends before the database is closed: a
	// backup or a notification is never cut off halfway.
	var bg sync.WaitGroup

	// Ad filter: on unless an admin turned it off in the settings.
	var adOn atomic.Bool
	v, _ := st.Setting("ad_filter")
	adOn.Store(v != "0")
	lists := cfg.AdFilterLists
	if len(lists) == 0 {
		lists = adblock.DefaultLists
	}
	ads := adblock.NewManager(cfg.DataDir, lists, adOn.Load)
	content.AdFilter = ads.Current
	bg.Go(func() { ads.Run(ctx) })

	pushSvc, err := push.NewService(st, cfg)
	if err != nil {
		return err
	}
	bg.Go(func() { pushSvc.Run(ctx) })

	fetcher := feeds.New(st, cfg)
	var instant *websub.Manager
	if !cfg.InstantOff {
		instant = websub.New(st, cfg.PublicURL, fetcher.Wake)
		fetcher.SetWebSub(instant)
		bg.Go(func() { instant.Run(ctx) })
	}
	fetchDone := make(chan struct{})
	go func() {
		fetcher.Run(ctx)
		close(fetchDone)
	}()

	apiSrv := api.New(cfg, st, fetcher, images, static, pushSvc, ads, &adOn)
	apiSrv.SetWebSub(instant)
	if setupCode != "" {
		apiSrv.SetSetupCode(setupCode)
		slog.Warn("no users yet: open the app in a browser and create the administrator with this setup code",
			"setup_code", auth.FormatSetupCode(setupCode), "address", cfg.ListenAddr)
		fmt.Fprintf(os.Stderr, "\n  Ebi Furai setup code: %s\n  Open the app in a browser to create the administrator.\n\n", auth.FormatSetupCode(setupCode))
	}
	if cfg.UpdateCheck {
		uc := update.New(version, cfg.UpdateCheckURL)
		if pushSvc != nil {
			uc.OnAvailable = func(found update.Status) {
				nctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				if _, err := pushSvc.NotifyUpdate(nctx, found.Latest); err != nil {
					slog.Warn("update notification failed", "err", err)
				}
			}
		}
		apiSrv.SetUpdates(uc)
		bg.Go(func() { uc.Run(ctx) })
	}
	if cfg.Backups {
		bk := backup.NewManager(st.DB, cfg.BackupDir, cfg.BackupDaily, cfg.BackupWeekly, cfg.Location)
		apiSrv.SetBackups(bk)
		apiSrv.SetRestart(func() {
			select {
			case restart <- struct{}{}:
			default:
			}
		})
		bg.Go(func() { bk.Run(ctx) })
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           apiSrv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr, "version", version, "build", static.Version, "go", runtime.Version())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var result error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	case <-restart:
		result = errRestart
	}
	slog.Info("shutting down")
	cancel()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	srv.Shutdown(shutdownCtx)
	<-fetchDone
	done := make(chan struct{})
	go func() { bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		slog.Warn("background work still running at shutdown")
	}
	return result
}

// errRestart: run ended to start again (a restored backup is waiting).
var errRestart = errors.New("restart")

// restartSelf runs this same program again in place of the current
// process (same PID: fine for Docker and systemd), once run has closed the
// database. The new start applies the restored backup.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	slog.Info("restarting")
	return syscall.Exec(exe, os.Args, os.Environ())
}

// bootstrapAdmin creates the first administrator when the database has no
// users yet and ADMIN_USER/ADMIN_PASSWORD are set. Without them it returns
// a setup code: the app then asks for it to create the administrator.
func bootstrapAdmin(cfg *config.Config, st *store.Store) (string, error) {
	n, err := st.CountUsers()
	if err != nil || n > 0 {
		return "", err
	}
	if cfg.AdminUser == "" || cfg.AdminPassword == "" {
		return auth.SetupCode(), nil
	}
	if err := auth.ValidatePassword(cfg.AdminPassword); err != nil {
		return "", errors.New("ADMIN_PASSWORD: " + err.Error())
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return "", err
	}
	if _, err := st.CreateUser(cfg.AdminUser, hash, true); err != nil {
		return "", err
	}
	slog.Info("admin user created", "username", cfg.AdminUser)
	return "", nil
}
