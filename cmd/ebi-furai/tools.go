package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/trapassati/ebi-furai/internal/backup"
	"github.com/trapassati/ebi-furai/internal/config"
	"github.com/trapassati/ebi-furai/internal/db"
	"github.com/trapassati/ebi-furai/internal/store"
)

// healthcheck is used by the Docker HEALTHCHECK (the image has no curl).
func healthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// runBackup makes a backup now. With no argument it is one of the automatic
// backups (compressed, in BACKUP_DIR, rotated with the others); with a path
// it writes a plain copy of the database there. Both are consistent while
// the server is running (VACUUM INTO).
func runBackup(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer conn.Close()
	if len(args) == 0 || args[0] == "" {
		info, err := backup.NewManager(conn, cfg.BackupDir, cfg.BackupDaily, cfg.BackupWeekly, cfg.Location).Create(context.Background())
		if err != nil {
			return err
		}
		fmt.Println(filepath.Join(cfg.BackupDir, info.Name))
		return nil
	}
	dest := args[0]
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if _, err := conn.Exec(`VACUUM INTO ?`, dest); err != nil {
		return err
	}
	fmt.Println(dest)
	return nil
}

// reset2FA turns off the two-step verification of a user who lost the
// phone and the recovery codes (also for the admin, who has nobody else to
// ask): docker compose exec ebi-furai /ebi-furai reset-2fa <user>
func reset2FA(args []string) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("usage: reset-2fa <user>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer conn.Close()
	st := store.New(conn)
	id, err := st.UserIDByName(args[0])
	if err != nil {
		return fmt.Errorf("user %q: %w", args[0], err)
	}
	if err := st.DisableTOTP(id); err != nil {
		return err
	}
	fmt.Printf("two-step verification turned off for %s\n", args[0])
	return nil
}
