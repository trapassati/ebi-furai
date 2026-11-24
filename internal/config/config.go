// Package config reads runtime settings from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultSourceURL is where the code is published. Who runs a modified
// version sets SOURCE_URL to their own copy (AGPL-3.0, section 13).
const DefaultSourceURL = "https://github.com/trapassati/ebi-furai"

// MinRefresh is the shortest feed refresh interval, for the server default
// and for the interval each user can choose.
const MinRefresh = 5 * time.Minute

type Config struct {
	ListenAddr      string
	DataDir         string
	AppName         string
	SourceURL       string // where the source code of this instance is published (AGPL-3.0, section 13)
	AdminUser       string
	AdminPassword   string
	RefreshInterval time.Duration
	FetchWorkers    int
	RetentionDays   int
	CookieSecure    string // "auto" (follow the request: https or not), "true" or "false"
	TrustCFHeader   bool
	ImageProxy      bool
	VAPIDSubject    string
	AdFilterLists   []string
	Location        *time.Location
	Backups         bool   // daily copies of the database
	BackupDir       string // where they go (default DATA_DIR/backups)
	BackupDaily     int    // how many daily copies are kept
	BackupWeekly    int    // how many weekly copies are kept after those
	UpdateCheck     bool   // ask GitHub now and then for a newer release
	UpdateCheckURL  string // where (tests; default the official repository)
	PublicURL       string // the address the server is reached at from the internet, without a final "/" (WebSub); empty = learned
	InstantOff      bool   // PUBLIC_URL=off: no WebSub subscriptions
}

func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "reader.db") }

// Load reads the configuration from the environment and applies defaults.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:     env("LISTEN_ADDR", ":9099"),
		DataDir:        env("DATA_DIR", "data"),
		AppName:        env("APP_NAME", "Ebi Furai"),
		SourceURL:      env("SOURCE_URL", DefaultSourceURL),
		AdminUser:      strings.TrimSpace(os.Getenv("ADMIN_USER")),
		AdminPassword:  os.Getenv("ADMIN_PASSWORD"),
		VAPIDSubject:   strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
		UpdateCheckURL: strings.TrimSpace(os.Getenv("UPDATE_CHECK_URL")),
		PublicURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/"),
	}
	for _, u := range strings.Split(os.Getenv("AD_FILTER_LISTS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			if !strings.HasPrefix(u, "https://") {
				return nil, fmt.Errorf("AD_FILTER_LISTS: %q must start with https://", u)
			}
			c.AdFilterLists = append(c.AdFilterLists, u)
		}
	}
	var err error
	if c.RefreshInterval, err = time.ParseDuration(env("REFRESH_INTERVAL", "20m")); err != nil {
		return nil, fmt.Errorf("REFRESH_INTERVAL: %w", err)
	}
	if c.RefreshInterval < MinRefresh {
		c.RefreshInterval = MinRefresh
	}
	if c.FetchWorkers, err = strconv.Atoi(env("FETCH_WORKERS", "4")); err != nil || c.FetchWorkers < 1 {
		return nil, fmt.Errorf("FETCH_WORKERS must be a positive integer")
	}
	if c.RetentionDays, err = strconv.Atoi(env("RETENTION_DAYS", "90")); err != nil || c.RetentionDays < 1 {
		return nil, fmt.Errorf("RETENTION_DAYS must be a positive integer")
	}
	c.BackupDir = env("BACKUP_DIR", filepath.Join(c.DataDir, "backups"))
	if c.BackupDaily, err = strconv.Atoi(env("BACKUP_KEEP_DAILY", "7")); err != nil || c.BackupDaily < 1 {
		return nil, fmt.Errorf("BACKUP_KEEP_DAILY must be a positive integer")
	}
	if c.BackupWeekly, err = strconv.Atoi(env("BACKUP_KEEP_WEEKLY", "4")); err != nil || c.BackupWeekly < 0 {
		return nil, fmt.Errorf("BACKUP_KEEP_WEEKLY must be zero or a positive integer")
	}
	for _, b := range []struct {
		key string
		dst *bool

		def bool
	}{
		{"TRUST_CF_HEADER", &c.TrustCFHeader, false},
		{"IMAGE_PROXY", &c.ImageProxy, true},
		{"BACKUPS", &c.Backups, true},
		{"UPDATE_CHECK", &c.UpdateCheck, true},
	} {
		if *b.dst, err = envBool(b.key, b.def); err != nil {
			return nil, err
		}
	}
	switch c.CookieSecure = strings.ToLower(env("COOKIE_SECURE", "auto")); c.CookieSecure {
	case "auto":
	case "1", "true", "yes", "on":
		c.CookieSecure = "true"
	case "0", "false", "no", "off":
		c.CookieSecure = "false"
	default:
		return nil, fmt.Errorf("COOKIE_SECURE: invalid value %q, use auto, true or false", os.Getenv("COOKIE_SECURE"))
	}

	if v := c.VAPIDSubject; v != "" && !strings.HasPrefix(v, "mailto:") && !strings.HasPrefix(v, "https://") {
		return nil, fmt.Errorf("VAPID_SUBJECT must start with mailto: or https://")
	}

	if c.SourceURL != "" {
		if u, err := url.Parse(c.SourceURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("SOURCE_URL: invalid address %q", c.SourceURL)
		}
	}
	switch strings.ToLower(c.PublicURL) {
	case "off", "false", "no", "0":
		c.PublicURL, c.InstantOff = "", true
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("PUBLIC_URL: invalid address %q, use for example https://reader.example.com", c.PublicURL)
		}
	}
	// TZ, or the system's zone (UTC in the Docker image).
	c.Location = time.Local
	if tz := env("TZ", ""); tz != "" {
		if c.Location, err = time.LoadLocation(tz); err != nil {
			return nil, fmt.Errorf("TZ: %w", err)
		}
	}
	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// envBool parses a boolean variable; an unrecognized value is an error, so
// a typo cannot silently fall back to the default.
func envBool(key string, def bool) (bool, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "":
		return def, nil
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%s: invalid value %q, use true or false", key, os.Getenv(key))
}
