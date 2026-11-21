package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// Errors of Check, shown to the administrator (translated by i18n).
var (
	ErrNotDatabase = errors.New("the file is not an Ebi Furai database")
	ErrDamaged     = errors.New("the database in the file is damaged")
	ErrTooNew      = errors.New("the backup comes from a newer version of Ebi Furai: update the app first")
	ErrNoAdmin     = errors.New("the backup has no active administrator: nobody could sign in")
)

// Info describes a database checked by Check.
type Info struct {
	Version  int `json:"version"`
	Users    int `json:"users"`
	Feeds    int `json:"feeds"`
	Articles int `json:"articles"`
}

// LatestVersion is the schema version of this build.
func LatestVersion() int {
	entries, _ := fs.ReadDir(migrationsFS, "migrations")
	latest := 0
	for _, e := range entries {
		prefix, _, _ := strings.Cut(e.Name(), "_")
		if v, err := strconv.Atoi(prefix); err == nil && v > latest {
			latest = v
		}
	}
	return latest
}

// Check opens a database file read-only and tells whether it can replace
// the current one: an intact SQLite file with Ebi Furai's tables, not from
// a newer version, with at least one administrator who can sign in.
func Check(path string) (Info, error) {
	var info Info
	conn, err := sql.Open(driverName, "file:"+path+"?mode=ro")
	if err != nil {
		return info, err
	}
	defer conn.Close()
	var res string
	if err := conn.QueryRow(`PRAGMA quick_check`).Scan(&res); err != nil {
		if strings.Contains(err.Error(), "not a database") || strings.Contains(err.Error(), "file is encrypted") {
			return info, ErrNotDatabase
		}
		return info, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	if res != "ok" {
		return info, ErrDamaged
	}
	for _, t := range []string{"schema_migrations", "users", "feeds", "articles", "sessions"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t).Scan(&n); err != nil || n == 0 {
			return info, ErrNotDatabase
		}
	}
	if err := conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&info.Version); err != nil {
		return info, ErrNotDatabase
	}
	if info.Version > LatestVersion() {
		return info, ErrTooNew
	}
	var admins int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled = 0`).Scan(&admins); err != nil {
		return info, ErrNotDatabase
	}
	if admins == 0 {
		return info, ErrNoAdmin
	}
	conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&info.Users)
	conn.QueryRow(`SELECT COUNT(*) FROM feeds`).Scan(&info.Feeds)
	conn.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&info.Articles)
	return info, nil
}
