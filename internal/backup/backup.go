// Package backup makes copies of the database while the server runs.
//
// A copy is taken with VACUUM INTO, which writes a consistent snapshot even
// while feeds are being fetched, then compressed with gzip. One is made when
// the newest is older than a day; the newest of each of the last days and
// of each of the last weeks are kept, the others are deleted.
package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Info describes a backup file.
type Info struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

const prefix = "ebi-furai-"
const layout = "2006-01-02-150405"

var nameRE = regexp.MustCompile(`^ebi-furai-\d{4}-\d{2}-\d{2}-\d{6}\.db\.gz$`)

// ErrNotFound is a backup name that does not exist (or is not one).
var ErrNotFound = errors.New("backup not found")

type Manager struct {
	db         *sql.DB
	dir        string
	keepDaily  int
	keepWeekly int
	loc        *time.Location
	mu         sync.Mutex // one backup at a time
	remote     remoteState
}

func NewManager(db *sql.DB, dir string, keepDaily, keepWeekly int, loc *time.Location) *Manager {
	if loc == nil {
		loc = time.Local
	}
	return &Manager{db: db, dir: dir, keepDaily: keepDaily, keepWeekly: keepWeekly, loc: loc}
}

// Dir is where the backups are written.
func (m *Manager) Dir() string { return m.dir }

// Run makes a backup whenever the newest one is more than a day old,
// checking every hour; the first check waits a few minutes after start.
func (m *Manager) Run(ctx context.Context) {
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if m.due() {
			if info, err := m.Create(ctx); err != nil {
				slog.Error("backup failed", "err", err)
			} else {
				slog.Info("backup created", "name", info.Name, "size", info.Size)
			}
		} else if m.syncDue() {
			m.syncSoon() // copies elsewhere: retry after a failure, or settings changed
		}
		timer.Reset(time.Hour)
	}
}

func (m *Manager) due() bool {
	list, err := m.List()
	if err != nil {
		return true
	}
	return len(list) == 0 || time.Since(list[0].Time) > 23*time.Hour+30*time.Minute
}

// Create writes a new backup and removes the ones no longer kept.
func (m *Manager) Create(ctx context.Context) (Info, error) { return m.create(ctx, true) }

// SafetyCopy writes a backup without removing any: made before a restore,
// it must not delete the backup about to be restored (only one a day is
// kept, and that one may be from today).
func (m *Manager) SafetyCopy(ctx context.Context) (Info, error) { return m.create(ctx, false) }

func (m *Manager) create(ctx context.Context, prune bool) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return Info{}, err
	}
	now := time.Now().In(m.loc)
	name := prefix + now.Format(layout) + ".db.gz"
	// Two backups in the same second (a safety copy right after a backup
	// made by hand): the second one takes the next second's name.
	for i := 0; i < 60; i++ {
		if _, err := os.Stat(filepath.Join(m.dir, name)); errors.Is(err, os.ErrNotExist) {
			break
		}
		now = now.Add(time.Second)
		name = prefix + now.Format(layout) + ".db.gz"
	}
	raw := filepath.Join(m.dir, ".snapshot.db")
	part := filepath.Join(m.dir, ".partial.gz")
	os.Remove(raw)
	defer os.Remove(raw)
	defer os.Remove(part)
	if _, err := m.db.ExecContext(ctx, `VACUUM INTO ?`, raw); err != nil {
		return Info{}, fmt.Errorf("snapshot: %w", err)
	}
	if err := gzipFile(raw, part); err != nil {
		return Info{}, err
	}
	final := filepath.Join(m.dir, name)
	if err := os.Rename(part, final); err != nil {
		return Info{}, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	if prune {
		m.prune()
	}
	m.syncSoon()
	return Info{Name: name, Size: st.Size(), Time: now}, nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	_, err = io.Copy(zw, in)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if serr := out.Sync(); err == nil {
		err = serr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// List returns the backups, newest first.
func (m *Manager) List() ([]Info, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	list := []Info{}
	for _, e := range entries {
		if e.IsDir() || !nameRE.MatchString(e.Name()) {
			continue
		}
		t, err := time.ParseInLocation(layout, e.Name()[len(prefix):len(prefix)+len(layout)], m.loc)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, Info{Name: e.Name(), Size: fi.Size(), Time: t})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list, nil
}

// Open returns a backup file for download.
func (m *Manager) Open(name string) (*os.File, Info, error) {
	if !nameRE.MatchString(name) {
		return nil, Info{}, ErrNotFound
	}
	list, err := m.List()
	if err != nil {
		return nil, Info{}, err
	}
	for _, b := range list {
		if b.Name == name {
			f, err := os.Open(filepath.Join(m.dir, name))
			if errors.Is(err, os.ErrNotExist) {
				return nil, Info{}, ErrNotFound
			}
			return f, b, err
		}
	}
	return nil, Info{}, ErrNotFound
}

// Keep returns which backups the rotation keeps (list newest first): the
// newest of each of the last keepDaily days that have a backup, then the
// newest of each older week, for keepWeekly weeks not already covered.
func Keep(list []Info, keepDaily, keepWeekly int, loc *time.Location) map[string]bool {
	keep := map[string]bool{}
	days := map[string]bool{}  // days with a kept backup
	weeks := map[string]bool{} // weeks with a kept backup
	weekly := 0
	for _, b := range list {
		t := b.Time.In(loc)
		day := t.Format("2006-01-02")
		y, w := t.ISOWeek()
		week := fmt.Sprintf("%d-%02d", y, w)
		if days[day] {
			continue // an older backup of a day already kept
		}
		if len(days) < keepDaily {
			days[day], weeks[week], keep[b.Name] = true, true, true
			continue
		}
		if weeks[week] || weekly >= keepWeekly {
			continue
		}
		weeks[week], keep[b.Name] = true, true
		weekly++
	}
	return keep
}

func (m *Manager) prune() {
	list, err := m.List()
	if err != nil {
		return
	}
	keep := Keep(list, m.keepDaily, m.keepWeekly, m.loc)
	for _, b := range list {
		if !keep[b.Name] {
			if err := os.Remove(filepath.Join(m.dir, b.Name)); err == nil {
				slog.Info("backup removed", "name", b.Name)
			}
		}
	}
}
