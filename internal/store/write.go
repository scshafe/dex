package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/schema"
)

// WriteRolodex is the single choke point for every mutation. It:
//  1. Schema-validates the in-memory rolodex (rejects bad data before disk).
//  2. Acquires a per-rolodex lockfile (prevents concurrent corruption).
//  3. Writes the JSON to a tempfile and renames it into place (atomic).
//
// The file is placed under <root>/<visibility>/<slug>.<short-id>.json. If a
// file with the same rolodex id already exists under that tier, it is
// overwritten in place; otherwise a new file is created. Files in other
// tiers with the same id are ignored (callers should use `dex promote`
// to move rolodexes between tiers).
func (s *Store) WriteRolodex(r model.Rolodex) error {
	dir, ok := s.tiers[r.Visibility]
	if !ok {
		return fmt.Errorf("store: unknown visibility %q", r.Visibility)
	}

	b, err := json.MarshalIndent(&r, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal: %w", err)
	}
	var parsed any
	if err := json.Unmarshal(b, &parsed); err != nil {
		return fmt.Errorf("store: re-parse for validation: %w", err)
	}
	if err := schema.Validate(parsed); err != nil {
		return fmt.Errorf("store: schema: %w", err)
	}

	lockPath := filepath.Join(dir, ".lock."+r.ID)
	lock, err := acquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("store: lock: %w", err)
	}
	defer releaseLock(lock, lockPath)

	target, err := findFileForID(dir, r.ID)
	if err != nil {
		return err
	}
	if target == "" {
		target = filepath.Join(dir, fmt.Sprintf("%s.%s.json", r.Slug, shortID(r.ID)))
	}

	tmp, err := os.CreateTemp(dir, ".tmp-write-*.json")
	if err != nil {
		return fmt.Errorf("store: create tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("store: write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: rename: %w", err)
	}
	return nil
}

// shortID returns the last 6 characters of the ULID for filename use.
func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

// findFileForID scans dir for a .json file containing the given rolodex
// id. Returns the path if found, empty string + nil error if not.
func findFileForID(dir, id string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("readdir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(b, &probe); err != nil {
			continue
		}
		if probe.ID == id {
			return path, nil
		}
	}
	return "", nil
}

// acquireLock opens the lockfile with O_CREATE|O_EXCL. If another writer
// already holds it, returns an error.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("acquire %s: %w", path, err)
	}
	return f, nil
}

// releaseLock removes the lockfile and closes the handle.
func releaseLock(f *os.File, path string) {
	_ = f.Close()
	_ = os.Remove(path)
}
