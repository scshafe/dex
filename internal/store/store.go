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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/schema"
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

// LoadTier reads every `*.json` file under the given visibility's tier
// directory, validates each against the embedded schema, and returns the
// parsed Rolodexes. Files with extension other than `.json` are skipped.
// Validation errors are returned as a single wrapped error containing the
// offending file's path.
func (s *Store) LoadTier(v model.Visibility) ([]model.Rolodex, error) {
	dir, ok := s.tiers[v]
	if !ok {
		return nil, fmt.Errorf("store: unknown visibility %q", v)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("store: read tier %s: %w", v, err)
	}

	var out []model.Rolodex
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		r, err := s.readRolodex(path)
		if err != nil {
			return nil, fmt.Errorf("store: %s: %w", path, err)
		}
		if r.Visibility != v {
			return nil, fmt.Errorf("store: %s: visibility %q does not match tier dir %q",
				path, r.Visibility, v)
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) readRolodex(path string) (model.Rolodex, error) {
	var zero model.Rolodex
	b, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	// Validate against schema first (preserves rich error from the validator),
	// then unmarshal into the typed struct.
	var parsed any
	if err := json.Unmarshal(b, &parsed); err != nil {
		return zero, fmt.Errorf("parse: %w", err)
	}
	if err := schema.Validate(parsed); err != nil {
		return zero, fmt.Errorf("schema: %w", err)
	}
	var r model.Rolodex
	if err := json.Unmarshal(b, &r); err != nil {
		return zero, fmt.Errorf("decode: %w", err)
	}
	return r, nil
}

const RootSlug = "root"

// MergedRoot constructs the merged root rolodex by union-ing the per-tier
// root rolodexes (those with slug == "root") in precedence order
// (private > personal > bundled). Ephemeral is excluded.
//
// Collisions are resolved by entry slug: a higher-precedence entry shadows
// a lower-precedence entry with the same slug. The shadowed entry is
// reachable via `<slug>@<visibility>` addressing in a later iteration of
// the verb surface — for now it's simply hidden.
func (s *Store) MergedRoot() (model.Rolodex, error) {
	tiers := []model.Visibility{
		model.VisibilityBundled,
		model.VisibilityPersonal,
		model.VisibilityPrivate,
	}

	bySlug := map[string]model.Entry{}
	for _, v := range tiers {
		rolodexes, err := s.LoadTier(v)
		if err != nil {
			return model.Rolodex{}, err
		}
		for _, r := range rolodexes {
			if r.Slug != RootSlug {
				continue
			}
			for _, e := range r.Entries {
				// Lower-precedence is loaded first; later iteration
				// overwrites (= shadows).
				bySlug[e.Slug] = e
			}
		}
	}

	out := model.Rolodex{
		SchemaVersion: 1,
		ID:            "", // synthesized; no on-disk identity
		Slug:          "merged-root",
		Label:         "Merged root",
		Visibility:    model.VisibilityBundled, // a label, not authoritative
		Entries:       make([]model.Entry, 0, len(bySlug)),
	}
	for _, e := range bySlug {
		out.Entries = append(out.Entries, e)
	}
	// Sort by slug for stable output.
	sort.Slice(out.Entries, func(i, j int) bool {
		return out.Entries[i].Slug < out.Entries[j].Slug
	})
	return out, nil
}
