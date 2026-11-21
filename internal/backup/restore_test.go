package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trapassati/ebi-furai/internal/db"
)

// A backup made by Create can be staged and applied; the database then
// holds what the backup held.
func TestRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reader.db")
	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('admin', 'x', 1, 0)`)
	conn.Exec(`INSERT INTO sessions(token_hash, user_id, csrf_token, created_at, expires_at, last_seen) VALUES('t', 1, 'c', 0, 9999999999, 0)`)
	m := NewManager(conn, filepath.Join(dir, "backups"), 7, 4, time.UTC)
	info, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Changes after the backup: they will be gone.
	conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('later', 'x', 0, 0)`)
	// The copy made before restoring keeps today's backup.
	safety, err := m.SafetyCopy(context.Background())
	if err != nil || safety.Name == info.Name {
		t.Fatalf("safety copy: %v %q", err, safety.Name)
	}
	if list, _ := m.List(); len(list) != 2 {
		t.Fatalf("backups after the safety copy: %d", len(list))
	}

	got, err := m.StageBackup(info.Name, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Users != 1 || got.Version != db.LatestVersion() {
		t.Fatalf("check: %+v", got)
	}
	if !Pending(dbPath) {
		t.Fatal("not pending")
	}
	conn.Close()
	applied, err := ApplyPending(dbPath)
	if err != nil || !applied {
		t.Fatalf("apply: %v %v", applied, err)
	}
	conn, err = db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatalf("users after restore: %d", n)
	}
	if again, _ := ApplyPending(dbPath); again {
		t.Fatal("applied twice")
	}
}

func TestStageRefuses(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reader.db")
	cases := map[string][]byte{
		"text":     []byte("hello, this is not a database"),
		"bad gzip": {0x1f, 0x8b, 0x08, 0, 0, 0},
		"empty":    {},
	}
	for name, data := range cases {
		if _, err := Stage(bytes.NewReader(data), dbPath); !errors.Is(err, db.ErrNotDatabase) && !errors.Is(err, db.ErrDamaged) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// A database without an administrator.
	other := filepath.Join(dir, "other.db")
	conn, err := db.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	conn.Exec(`INSERT INTO users(username, password_hash, is_admin, created_at) VALUES('u', 'x', 0, 0)`)
	conn.Close()
	data, _ := os.ReadFile(other)
	if _, err := Stage(bytes.NewReader(data), dbPath); !errors.Is(err, db.ErrNoAdmin) {
		t.Errorf("no admin: got %v", err)
	}
	// From a newer version.
	conn, _ = db.Open(other)
	conn.Exec(`UPDATE users SET is_admin = 1`)
	conn.Exec(`INSERT INTO schema_migrations(version) VALUES(9999)`)
	conn.Close()
	data, _ = os.ReadFile(other)
	if _, err := Stage(bytes.NewReader(data), dbPath); !errors.Is(err, db.ErrTooNew) {
		t.Errorf("newer: got %v", err)
	}
	if Pending(dbPath) {
		t.Error("a refused file must not be staged")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "partial") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}
