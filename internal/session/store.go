package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Manager owns the on-disk session directory.
type Manager struct {
	dir     string
	entropy *ulid.MonotonicEntropy
}

// NewManager constructs a Manager rooted at dir. The directory is
// created if missing. entropy is the ULID source — the caller chooses
// it so production code can use crypto/rand and tests can swap in a
// deterministic source.
func NewManager(dir string, entropy *ulid.MonotonicEntropy) *Manager {
	_ = os.MkdirAll(dir, 0o755) // best-effort; surfaced on first write
	return &Manager{dir: dir, entropy: entropy}
}

func (m *Manager) Dir() string { return m.dir }

// NewSession creates a fresh State, writes it to disk, and returns
// it. Runs opportunistic GC on the session dir first (pinned
// decision #2).
func (m *Manager) NewSession() (State, error) {
	// GC is opportunistic; failures are non-fatal and silent in v1
	// (revisit once telemetry lands).
	_ = m.gc()

	id, err := m.newID()
	if err != nil {
		return State{}, err
	}
	now := time.Now()
	st := State{
		ID:              id,
		Cursor:          Cursor{Mode: CursorModeBrowse},
		Resolved:        map[string]string{},
		PendingConcerns: []PendingConcern{},
		Version:         0,
		CreatedAt:       now,
		LastTouched:     now,
		PreviousCursors: []Cursor{},
	}
	if err := m.Save(st); err != nil {
		return State{}, err
	}
	return st, nil
}

func (m *Manager) newID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), m.entropy)
	if err != nil {
		return "", fmt.Errorf("session: ulid: %w", err)
	}
	return "ses_" + id.String(), nil
}

func (m *Manager) sessionPath(id string) string {
	return filepath.Join(m.dir, id+".json")
}

// Save serializes st to disk via tempfile+rename (atomic).
func (m *Manager) Save(st State) error {
	if !strings.HasPrefix(st.ID, "ses_") {
		return fmt.Errorf("session: id %q missing ses_ prefix", st.ID)
	}
	b, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(m.dir, ".tmp-session-*.json")
	if err != nil {
		return fmt.Errorf("session: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("session: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("session: close: %w", err)
	}
	if err := os.Rename(tmpPath, m.sessionPath(st.ID)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("session: rename: %w", err)
	}
	return nil
}

// Load reads a session by id. Returns an error if the file is missing
// or unparseable.
func (m *Manager) Load(id string) (State, error) {
	b, err := os.ReadFile(m.sessionPath(id))
	if err != nil {
		return State{}, fmt.Errorf("session: load %s: %w", id, err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("session: parse %s: %w", id, err)
	}
	return st, nil
}

// End removes the session file. Missing files are not an error.
func (m *Manager) End(id string) error {
	err := os.Remove(m.sessionPath(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session: end %s: %w", id, err)
	}
	return nil
}

// gc removes session files whose last_touched is older than SessionTTL.
// Errors on individual files are swallowed — GC is opportunistic, not
// load-bearing.
func (m *Manager) gc() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("session: gc readdir: %w", err)
	}
	cutoff := time.Now().Add(-SessionTTL)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "ses_") {
			continue
		}
		path := filepath.Join(m.dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var probe struct {
			LastTouched time.Time `json:"last_touched"`
		}
		if err := json.Unmarshal(b, &probe); err != nil {
			continue
		}
		if probe.LastTouched.Before(cutoff) {
			_ = os.Remove(path)
		}
	}
	return nil
}
