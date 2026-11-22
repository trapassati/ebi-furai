package backup

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Remote is a folder elsewhere (a WebDAV server) where copies of the
// backups are kept, with the same rotation as here.
type Remote interface {
	Upload(ctx context.Context, name, file string) error
	List(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, name string) error
	URL() string
}

// RemoteStatus is how the copies elsewhere are doing.
type RemoteStatus struct {
	Syncing   bool      `json:"syncing"`
	LastOK    time.Time `json:"last_ok,omitzero"`
	LastError string    `json:"last_error,omitempty"`
	ErrorAt   time.Time `json:"error_at,omitzero"`
	Count     int       `json:"count"`
	Newest    string    `json:"newest,omitempty"`
}

type remoteState struct {
	provider func() (Remote, error) // nil Remote = not configured
	mu       sync.Mutex             // one sync at a time
	smu      sync.Mutex             // guards status and dirty
	status   RemoteStatus
	dirty    bool
}

// SetRemote tells the manager where to copy the backups. The function is
// asked at every sync, so a change of settings applies at once; it returns
// a nil Remote when copies elsewhere are off.
func (m *Manager) SetRemote(provider func() (Remote, error)) {
	m.remote.provider = provider
	m.remote.smu.Lock()
	m.remote.dirty = true
	m.remote.smu.Unlock()
}

// RemoteStatus returns the state of the copies elsewhere.
func (m *Manager) RemoteStatus() RemoteStatus {
	m.remote.smu.Lock()
	defer m.remote.smu.Unlock()
	return m.remote.status
}

// ResetRemote forgets the status (settings changed or removed) and marks a
// sync as needed.
func (m *Manager) ResetRemote() {
	m.remote.smu.Lock()
	m.remote.status = RemoteStatus{}
	m.remote.dirty = true
	m.remote.smu.Unlock()
}

func (m *Manager) setStatus(fn func(*RemoteStatus)) {
	m.remote.smu.Lock()
	fn(&m.remote.status)
	m.remote.smu.Unlock()
}

// syncSoon runs a sync in the background (after a new backup).
func (m *Manager) syncSoon() {
	if m.remote.provider == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if err := m.Sync(ctx); err != nil {
			slog.Warn("backup copy elsewhere failed", "err", err)
		}
	}()
}

// syncDue reports whether the hourly check should sync: something changed,
// or the last attempt failed.
func (m *Manager) syncDue() bool {
	m.remote.smu.Lock()
	defer m.remote.smu.Unlock()
	return m.remote.dirty || m.remote.status.LastError != ""
}

// Sync uploads the backups the remote folder does not have yet (newest
// first), then removes there the ones the rotation no longer keeps, and
// leftovers of broken uploads.
func (m *Manager) Sync(ctx context.Context) error {
	if m.remote.provider == nil {
		return nil
	}
	rm, err := m.remote.provider()
	if err != nil || rm == nil {
		if err != nil {
			m.setStatus(func(st *RemoteStatus) { st.LastError, st.ErrorAt = err.Error(), time.Now() })
		}
		return err
	}
	m.remote.mu.Lock()
	defer m.remote.mu.Unlock()
	m.setStatus(func(st *RemoteStatus) { st.Syncing = true })
	m.remote.smu.Lock()
	m.remote.dirty = false
	m.remote.smu.Unlock()
	err = m.sync(ctx, rm)
	m.setStatus(func(st *RemoteStatus) {
		st.Syncing = false
		if err != nil {
			st.LastError, st.ErrorAt = err.Error(), time.Now()
		} else {
			st.LastError, st.ErrorAt, st.LastOK = "", time.Time{}, time.Now()
		}
	})
	return err
}

func (m *Manager) sync(ctx context.Context, rm Remote) error {
	names, err := rm.List(ctx)
	if err != nil {
		return err
	}
	there := map[string]bool{}
	for _, n := range names {
		there[n] = true
	}
	local, err := m.List()
	if err != nil {
		return err
	}
	for _, b := range local { // newest first
		if there[b.Name] {
			continue
		}
		if err := rm.Upload(ctx, b.Name, filepath.Join(m.dir, b.Name)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // deleted here meanwhile
			}
			return err
		}
		there[b.Name] = true
		slog.Info("backup copied elsewhere", "name", b.Name, "to", rm.URL())
	}
	// The rotation, over what is there now.
	remote := []Info{}
	for n := range there {
		if strings.HasSuffix(n, ".db.gz.part") && strings.HasPrefix(n, prefix) {
			rm.Delete(ctx, n) // a broken upload
			continue
		}
		if !nameRE.MatchString(n) {
			continue // not ours: never touched
		}
		t, err := time.ParseInLocation(layout, n[len(prefix):len(prefix)+len(layout)], m.loc)
		if err != nil {
			continue
		}
		remote = append(remote, Info{Name: n, Time: t})
	}
	sort.Slice(remote, func(i, j int) bool { return remote[i].Time.After(remote[j].Time) })
	keep := Keep(remote, m.keepDaily, m.keepWeekly, m.loc)
	kept := 0
	for _, b := range remote {
		if keep[b.Name] {
			kept++
			continue
		}
		if err := rm.Delete(ctx, b.Name); err != nil {
			return err
		}
		slog.Info("backup copy removed elsewhere", "name", b.Name)
	}
	m.setStatus(func(st *RemoteStatus) {
		st.Count = kept
		st.Newest = ""
		if len(remote) > 0 {
			st.Newest = remote[0].Name
		}
	})
	return nil
}

// Delete removes a backup from this server's folder (not its copy
// elsewhere).
func (m *Manager) Delete(name string) error {
	if !nameRE.MatchString(name) {
		return ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err := os.Remove(filepath.Join(m.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err == nil {
		slog.Info("backup deleted", "name", name)
	}
	return err
}
