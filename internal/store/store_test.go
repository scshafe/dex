package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

func TestOpenMissingDir(t *testing.T) {
	tmp := t.TempDir()
	_, err := store.Open(filepath.Join(tmp, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error opening missing dir")
	}
}

func TestOpenEmptyStore(t *testing.T) {
	tmp := t.TempDir()
	for _, tier := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(tmp, tier), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", tier, err)
		}
	}
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tiers := s.Tiers()
	if len(tiers) != 4 {
		t.Fatalf("tiers: got %d want 4", len(tiers))
	}
	for _, v := range []model.Visibility{
		model.VisibilityBundled, model.VisibilityPersonal,
		model.VisibilityPrivate, model.VisibilityEphemeral,
	} {
		if _, ok := tiers[v]; !ok {
			t.Fatalf("missing tier %s", v)
		}
	}
}

func TestOpenMissingTierDir(t *testing.T) {
	// Only bundled present; others auto-created.
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "bundled"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(s.Tiers()) != 4 {
		t.Fatalf("expected 4 tier dirs (auto-created), got %d", len(s.Tiers()))
	}
}

func TestLoadTier(t *testing.T) {
	s, err := store.Open("testdata/simple")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rolodexes, err := s.LoadTier(model.VisibilityBundled)
	if err != nil {
		t.Fatalf("load bundled: %v", err)
	}
	if len(rolodexes) != 1 {
		t.Fatalf("rolodexes: got %d want 1", len(rolodexes))
	}
	r := rolodexes[0]
	if r.Slug != "root" {
		t.Fatalf("slug: got %q want root", r.Slug)
	}
	if r.Visibility != model.VisibilityBundled {
		t.Fatalf("visibility: got %q want bundled", r.Visibility)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("entries: got %d want 1", len(r.Entries))
	}
}

func TestLoadTierEmpty(t *testing.T) {
	tmp := t.TempDir()
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rolodexes, err := s.LoadTier(model.VisibilityBundled)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(rolodexes) != 0 {
		t.Fatalf("expected 0 rolodexes, got %d", len(rolodexes))
	}
}

func TestLoadTierRejectsInvalid(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "bundled"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"schema_version":1,"slug":"missing-id","label":"x","visibility":"bundled","entries":[]}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.LoadTier(model.VisibilityBundled)
	if err == nil {
		t.Fatal("expected schema-validation error on missing id")
	}
}
