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
