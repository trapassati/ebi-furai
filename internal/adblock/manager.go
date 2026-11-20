package adblock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/trapassati/ebi-furai/internal/netsafe"
)

const (
	maxListSize    = 20 << 20
	updateInterval = 24 * time.Hour
)

// DefaultLists: EasyList plus the Italian supplement.
var DefaultLists = []string{
	"https://easylist.to/easylist/easylist.txt",
	"https://easylist-downloads.adblockplus.org/easylistitaly.txt",
}

// Manager keeps the lists cached in dir and refreshed once a day.
type Manager struct {
	dir     string
	lists   []string
	client  *http.Client
	enabled func() bool

	cur     atomic.Pointer[Filter]
	mu      sync.Mutex
	updated time.Time
	lastErr string
}

func NewManager(dataDir string, lists []string, enabled func() bool) *Manager {
	return &Manager{dir: filepath.Join(dataDir, "filters"), lists: lists, client: netsafe.NewClient(60 * time.Second), enabled: enabled}
}

// Current returns the active filter, or nil when filtering is off or no
// list is available yet.
func (m *Manager) Current() *Filter {
	if m == nil || !m.enabled() {
		return nil
	}
	return m.cur.Load()
}

func (m *Manager) file(u string) string {
	sum := sha256.Sum256([]byte(u))
	return filepath.Join(m.dir, hex.EncodeToString(sum[:6])+".txt")
}

// load builds the filter from the cached files.
func (m *Manager) load() error {
	f := New()
	var newest time.Time
	found := false
	for _, u := range m.lists {
		data, err := os.ReadFile(m.file(u))
		if err != nil {
			continue
		}
		found = true
		if st, err := os.Stat(m.file(u)); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
		if err := f.Add(bytes.NewReader(data)); err != nil {
			slog.Warn("adblock: list not parsed", "url", u, "err", err)
		}
	}
	if !found {
		return errors.New("no list downloaded")
	}
	m.cur.Store(f)
	m.mu.Lock()
	m.updated = newest
	m.mu.Unlock()
	slog.Info("adblock: lists loaded", "rules", f.Rules)
	return nil
}

// Update downloads the lists, keeping the cached copy of any that fails.
func (m *Manager) Update(ctx context.Context) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	var errs []error
	for _, u := range m.lists {
		if err := m.download(ctx, u); err != nil {
			slog.Warn("adblock: download failed", "url", u, "err", err)
			errs = append(errs, err)
		}
	}
	err := errors.Join(errs...)
	m.mu.Lock()
	m.lastErr = ""
	if err != nil {
		m.lastErr = err.Error()
	}
	m.mu.Unlock()
	if lerr := m.load(); lerr != nil {
		return errors.Join(err, lerr)
	}
	return err
}

func (m *Manager) download(ctx context.Context, u string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := netsafe.Get(ctx, m.client, u, map[string]string{"Accept": "text/plain"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: risposta %d", u, resp.StatusCode)
	}
	data, err := netsafe.ReadLimited(resp.Body, maxListSize)
	if err != nil {
		return err
	}
	if !bytes.Contains(data[:min(len(data), 4096)], []byte("[Adblock")) && !bytes.Contains(data[:min(len(data), 4096)], []byte("! Title")) {
		return fmt.Errorf("%s: does not look like a filter list", u)
	}
	tmp := m.file(u) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.file(u))
}

// Run loads the cache, then keeps the lists up to date.
func (m *Manager) Run(ctx context.Context) {
	_ = m.load()
	for {
		m.mu.Lock()
		age := time.Since(m.updated)
		m.mu.Unlock()
		wait := updateInterval - age
		if m.cur.Load() == nil || wait <= 0 {
			wait = updateInterval
			if err := m.Update(ctx); err != nil && m.cur.Load() == nil {
				wait = time.Hour // nothing usable yet: retry sooner
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

type Status struct {
	Enabled bool      `json:"enabled"`
	Rules   int       `json:"rules"`
	Removed int64     `json:"removed"`
	Updated time.Time `json:"updated"`
	Error   string    `json:"error,omitempty"`
}

func (m *Manager) Status() Status {
	st := Status{Enabled: m.enabled()}
	if f := m.cur.Load(); f != nil {
		st.Rules = f.Rules
		st.Removed = f.Removed()
	}
	m.mu.Lock()
	st.Updated = m.updated
	st.Error = m.lastErr
	m.mu.Unlock()
	return st
}
