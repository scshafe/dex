package session_test

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/scshafe/dex/internal/session"
)

func newManager(t *testing.T) *session.Manager {
	t.Helper()
	dir := t.TempDir()
	return session.NewManager(dir, ulidEntropy())
}

func ulidEntropy() *ulid.MonotonicEntropy {
	return ulid.Monotonic(rand.Reader, 0)
}

func TestNewSessionWritesFile(t *testing.T) {
	m := newManager(t)
	s, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if !strings.HasPrefix(s.ID, "ses_") {
		t.Fatalf("session id %q lacks ses_ prefix", s.ID)
	}
	files, err := os.ReadDir(m.Dir())
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(files))
	}
}

func TestLoadRoundTripsState(t *testing.T) {
	m := newManager(t)
	s, err := m.NewSession()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	s.Resolved["ns"] = "prod"
	s.Version = 7
	if err := m.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := m.Load(s.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != 7 {
		t.Fatalf("version: got %d want 7", loaded.Version)
	}
	if loaded.Resolved["ns"] != "prod" {
		t.Fatalf("resolved[ns]: got %q", loaded.Resolved["ns"])
	}
}

func TestEndRemovesFile(t *testing.T) {
	m := newManager(t)
	s, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := m.End(s.ID); err != nil {
		t.Fatalf("end: %v", err)
	}
	if _, err := m.Load(s.ID); err == nil {
		t.Fatalf("load after end should fail")
	}
}

func TestNewSessionGCsExpiredFiles(t *testing.T) {
	m := newManager(t)
	expired, _ := m.NewSession()
	// Backdate the on-disk file's last_touched to 31 minutes ago.
	expired.LastTouched = time.Now().Add(-31 * time.Minute)
	if err := m.Save(expired); err != nil {
		t.Fatalf("backdate save: %v", err)
	}

	// Creating a new session triggers GC and should remove the
	// expired one.
	if _, err := m.NewSession(); err != nil {
		t.Fatalf("new session: %v", err)
	}

	if _, err := m.Load(expired.ID); err == nil {
		t.Fatalf("expired session should have been GC'd")
	}
	files, _ := os.ReadDir(m.Dir())
	if len(files) != 1 {
		t.Fatalf("expected 1 file (only the new one), got %d", len(files))
	}
}

func TestListReturnsAllSessions(t *testing.T) {
	m := newManager(t)
	a, err := m.NewSession()
	if err != nil {
		t.Fatalf("new a: %v", err)
	}
	b, err := m.NewSession()
	if err != nil {
		t.Fatalf("new b: %v", err)
	}

	list, err := m.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len: got %d want 2", len(list))
	}
	ids := map[string]bool{list[0].ID: true, list[1].ID: true}
	if !ids[a.ID] || !ids[b.ID] {
		t.Fatalf("list missing one of the created sessions: got ids=%v want %q and %q",
			ids, a.ID, b.ID)
	}
}

func TestListIgnoresNonSessionFiles(t *testing.T) {
	m := newManager(t)
	if _, err := m.NewSession(); err != nil {
		t.Fatalf("new: %v", err)
	}
	// Drop a non-session file in the dir.
	if err := os.WriteFile(filepath.Join(m.Dir(), "junk.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := m.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len: got %d want 1 (the junk file should be ignored)", len(list))
	}
}
