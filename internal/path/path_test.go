package path_test

import (
	"errors"
	"testing"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/path"
)

// fakeResolver is an in-memory Resolver for algorithm-level tests.
// Tasks 2+ exercise pointer following through it; Task 1 only needs the
// merged root to walk.
type fakeResolver struct {
	rolodexes map[string]model.Rolodex
}

func (f *fakeResolver) LookupByID(id string) (model.Rolodex, bool, error) {
	r, ok := f.rolodexes[id]
	return r, ok, nil
}

func mergedRoot(entries ...model.Entry) model.Rolodex {
	return model.Rolodex{
		SchemaVersion: 1,
		Slug:          "merged-root",
		Label:         "Merged root",
		Visibility:    model.VisibilityBundled,
		Entries:       entries,
	}
}

func TestResolveSingleSegmentPointer(t *testing.T) {
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{
			ID:    "01HB00000000000000000000E1",
			Slug:  "tools",
			Label: "Tools",
		},
		Kind:    model.KindPointer,
		Pointer: &model.PointerPayload{To: "01HB00000000000000000000T1"},
	})

	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	result, err := path.Resolve(r, root, "/tools")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Entry.Slug != "tools" {
		t.Fatalf("entry.slug: got %q want tools", result.Entry.Slug)
	}
	if result.Entry.Kind != model.KindPointer {
		t.Fatalf("entry.kind: got %q want pointer", result.Entry.Kind)
	}
}

func TestResolveSingleSegmentNonPointer(t *testing.T) {
	// Final segment may be any kind, per architect Q3.
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{
			ID:    "01HB00000000000000000000E2",
			Slug:  "readme",
			Label: "Readme",
		},
		Kind: model.KindInfo,
		Info: &model.InfoPayload{Content: "hi"},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	result, err := path.Resolve(r, root, "/readme")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Entry.Kind != model.KindInfo {
		t.Fatalf("entry.kind: got %q want info", result.Entry.Kind)
	}
}

func TestResolveNotFound(t *testing.T) {
	root := mergedRoot()
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	_, err := path.Resolve(r, root, "/missing")
	if !errors.Is(err, path.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveEmptyPath(t *testing.T) {
	root := mergedRoot()
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	for _, p := range []string{"", "/"} {
		if _, err := path.Resolve(r, root, p); err == nil {
			t.Fatalf("expected error on empty path %q", p)
		}
	}
}

func TestResolveRequiresLeadingSlash(t *testing.T) {
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000E1", Slug: "tools", Label: "Tools"},
		Kind:     model.KindPointer,
		Pointer:  &model.PointerPayload{To: "01HB00000000000000000000T1"},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	if _, err := path.Resolve(r, root, "tools"); err == nil {
		t.Fatalf("expected error on path without leading slash")
	}
}

func TestResolveTrailingSlashIgnored(t *testing.T) {
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000E1", Slug: "tools", Label: "Tools"},
		Kind:     model.KindPointer,
		Pointer:  &model.PointerPayload{To: "01HB00000000000000000000T1"},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	result, err := path.Resolve(r, root, "/tools/")
	if err != nil {
		t.Fatalf("resolve with trailing slash: %v", err)
	}
	if result.Entry.Slug != "tools" {
		t.Fatalf("entry.slug: got %q want tools", result.Entry.Slug)
	}
}
