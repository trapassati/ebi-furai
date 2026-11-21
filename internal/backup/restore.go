package backup

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/trapassati/ebi-furai/internal/db"
)

// Restoring replaces the whole database. It happens in two steps, because
// the database is in use while the server runs:
//
//  1. Stage writes the backup next to the database as "<db>.restore",
//     after checking it (db.Check);
//  2. at the next start, before opening the database, ApplyPending puts it
//     in place. The server restarts itself right after step 1.
//
// The current database is saved as a normal backup before step 1, so a
// restore can always be undone by restoring that one.

// MaxRestoreSize limits what a backup may expand to (protects the disk
// from a crafted gzip file).
const MaxRestoreSize = 8 << 30

// ErrTooLarge: the file is over the limits.
var ErrTooLarge = errors.New("the backup is too large")

func pendingPath(dbPath string) string { return dbPath + ".restore" }

// Stage checks a backup (gzip, as made by Create, or a plain SQLite file)
// and prepares it to replace the database at the next start.
func Stage(src io.Reader, dbPath string) (db.Info, error) {
	br := bufio.NewReader(src)
	var r io.Reader = br
	if magic, _ := br.Peek(2); bytes.Equal(magic, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return db.Info{}, db.ErrNotDatabase
		}
		defer zr.Close()
		r = zr
	}
	part := dbPath + ".restore.partial"
	// The check may leave SQLite's side files next to it.
	defer func() {
		for _, f := range []string{part, part + "-wal", part + "-shm", part + "-journal"} {
			os.Remove(f)
		}
	}()
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return db.Info{}, err
	}
	n, err := io.Copy(out, io.LimitReader(r, MaxRestoreSize+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if errors.Is(err, gzip.ErrChecksum) || errors.Is(err, gzip.ErrHeader) || errors.Is(err, io.ErrUnexpectedEOF) {
			return db.Info{}, db.ErrDamaged
		}
		return db.Info{}, err
	}
	if n > MaxRestoreSize {
		return db.Info{}, ErrTooLarge
	}
	info, err := db.Check(part)
	if err != nil {
		return info, err
	}
	if err := os.Rename(part, pendingPath(dbPath)); err != nil {
		return info, err
	}
	return info, nil
}

// StageBackup stages one of the backups in the folder.
func (m *Manager) StageBackup(name, dbPath string) (db.Info, error) {
	f, _, err := m.Open(name)
	if err != nil {
		return db.Info{}, err
	}
	defer f.Close()
	return Stage(f, dbPath)
}

// Pending tells whether a restore waits for the next start.
func Pending(dbPath string) bool {
	_, err := os.Stat(pendingPath(dbPath))
	return err == nil
}

// ApplyPending puts a staged backup in place of the database. It must run
// before the database is opened. It returns true when it did.
func ApplyPending(dbPath string) (bool, error) {
	pending := pendingPath(dbPath)
	if _, err := os.Stat(pending); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	// The write-ahead log belongs to the old database: it must go.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("restore: %w", err)
		}
	}
	if err := os.Rename(pending, dbPath); err != nil {
		return false, fmt.Errorf("restore: %w", err)
	}
	return true, nil
}
