// Package store reads dex rolodex files from disk, organized by
// visibility tier.
//
// Layout (rooted at the store path):
//
//   <root>/bundled/<slug>.<short>.json
//   <root>/personal/<slug>.<short>.json
//   <root>/private/<slug>.<short>.json
//   <root>/ephemeral/<slug>.<short>.json
//
// Tier directories are created on Open if missing. This makes a fresh
// install zero-friction: `mkdir ~/.local/share/dex && dex ls` works.
package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scshafe/dex/internal/model"
)

type Store struct {
	root  string
	tiers map[model.Visibility]string
}

func Open(root string) (*Store, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("store: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("store: root %q is not a directory", root)
	}

	tiers := map[model.Visibility]string{
		model.VisibilityBundled:   filepath.Join(root, "bundled"),
		model.VisibilityPersonal:  filepath.Join(root, "personal"),
		model.VisibilityPrivate:   filepath.Join(root, "private"),
		model.VisibilityEphemeral: filepath.Join(root, "ephemeral"),
	}
	for v, p := range tiers {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s tier: %w", v, err)
		}
	}
	return &Store{root: root, tiers: tiers}, nil
}

func (s *Store) Root() string { return s.root }

// Tiers returns the tier-directory map. The returned map is a fresh copy;
// callers may not mutate the Store via this method.
func (s *Store) Tiers() map[model.Visibility]string {
	out := make(map[model.Visibility]string, len(s.tiers))
	for k, v := range s.tiers {
		out[k] = v
	}
	return out
}
