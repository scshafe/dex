package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

func newWritableStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	tmp := t.TempDir()
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s, tmp
}

func sampleRolodex(id, slug string, v model.Visibility) model.Rolodex {
	return model.Rolodex{
		SchemaVersion: 1,
		ID:            id,
		Slug:          slug,
		Label:         "Sample",
		Visibility:    v,
		Entries:       []model.Entry{},
	}
}

func TestWriteRolodexRoundTrip(t *testing.T) {
	s, root := newWritableStore(t)
	r := sampleRolodex("01HB00000000000000000000R1", "root", model.VisibilityBundled)
	if err := s.WriteRolodex(r); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "bundled"))
	if err != nil {
		t.Fatal(err)
	}
	var jsonFiles []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}
	if len(jsonFiles) != 1 {
		t.Fatalf("got %d JSON files, want 1: %v", len(jsonFiles), jsonFiles)
	}
	all, err := s.LoadTier(model.VisibilityBundled)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 1 || all[0].ID != r.ID {
		t.Fatalf("loaded back: got %+v", all)
	}
}

func TestWriteRolodexRejectsInvalid(t *testing.T) {
	s, _ := newWritableStore(t)
	r := sampleRolodex("01HB00000000000000000000R1", "BadSlug", model.VisibilityBundled)
	if err := s.WriteRolodex(r); err == nil {
		t.Fatal("expected schema validation to reject uppercase slug")
	}
}

func TestWriteRolodexUpdatesExistingFile(t *testing.T) {
	s, root := newWritableStore(t)
	r := sampleRolodex("01HB00000000000000000000R1", "root", model.VisibilityBundled)
	if err := s.WriteRolodex(r); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	r.Label = "Updated"
	if err := s.WriteRolodex(r); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "bundled"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("got %d JSON files, want 1", count)
	}
	all, _ := s.LoadTier(model.VisibilityBundled)
	if all[0].Label != "Updated" {
		t.Fatalf("label not updated: %q", all[0].Label)
	}
}

func TestWriteRolodexAtomicOnConcurrentWrites(t *testing.T) {
	s, root := newWritableStore(t)
	const id = "01HB00000000000000000000R1"
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			r := sampleRolodex(id, "root", model.VisibilityBundled)
			r.Label = label
			_ = s.WriteRolodex(r)
		}("label-" + filepath.Base(t.TempDir()))
	}
	wg.Wait()

	files, err := filepath.Glob(filepath.Join(root, "bundled", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %v", len(files), files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var got model.Rolodex
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("final file is corrupt: %v\n%s", err, string(b))
	}
	if got.ID != id {
		t.Fatalf("final file has wrong id %q", got.ID)
	}
}

func TestWriteRolodexFilenamePattern(t *testing.T) {
	s, root := newWritableStore(t)
	r := sampleRolodex("01HB00000000000000000000R1", "broker-providers", model.VisibilityBundled)
	if err := s.WriteRolodex(r); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "bundled", "broker-providers.*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected file named broker-providers.<short>.json; matches=%v", matches)
	}
}
