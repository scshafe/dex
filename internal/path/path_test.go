package path_test

import (
	"errors"
	"strings"
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

func TestResolveTwoSegmentThroughPointer(t *testing.T) {
	// Root has /tools which is a pointer to a "tools" rolodex containing /readme.
	toolsRolodex := model.Rolodex{
		SchemaVersion: 1,
		ID:            "01HB00000000000000000000T1",
		Slug:          "tools",
		Label:         "Tools",
		Visibility:    model.VisibilityBundled,
		Entries: []model.Entry{
			{
				NodeCore: model.NodeCore{ID: "01HB00000000000000000000E2", Slug: "readme", Label: "Readme"},
				Kind:     model.KindInfo,
				Info:     &model.InfoPayload{Content: "tools readme"},
			},
		},
	}
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000E1", Slug: "tools", Label: "Tools"},
		Kind:     model.KindPointer,
		Pointer:  &model.PointerPayload{To: toolsRolodex.ID},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{toolsRolodex.ID: toolsRolodex}}

	result, err := path.Resolve(r, root, "/tools/readme")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Entry.Slug != "readme" {
		t.Fatalf("entry.slug: got %q want readme", result.Entry.Slug)
	}
	if result.Entry.Kind != model.KindInfo {
		t.Fatalf("entry.kind: got %q want info", result.Entry.Kind)
	}
	if result.ParentRolodex.ID != toolsRolodex.ID {
		t.Fatalf("parent.id: got %q want %q", result.ParentRolodex.ID, toolsRolodex.ID)
	}
}

func TestResolveTraversesNonPointer(t *testing.T) {
	// /readme/x — readme is an info entry, can't be drilled.
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000E1", Slug: "readme", Label: "Readme"},
		Kind:     model.KindInfo,
		Info:     &model.InfoPayload{Content: "hi"},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	_, err := path.Resolve(r, root, "/readme/x")
	if !errors.Is(err, path.ErrTraversesNonPointer) {
		t.Fatalf("expected ErrTraversesNonPointer, got %v", err)
	}
}

func TestResolveDanglingPointer(t *testing.T) {
	// /tools points at a uuid that the Resolver doesn't have.
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000E1", Slug: "tools", Label: "Tools"},
		Kind:     model.KindPointer,
		Pointer:  &model.PointerPayload{To: "01HB00000000000000000000XX"},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{}}
	_, err := path.Resolve(r, root, "/tools/x")
	if !errors.Is(err, path.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on dangling pointer, got %v", err)
	}
}

func TestResolveCycle(t *testing.T) {
	// Build a 2-rolodex cycle: A -> B -> A.
	a := model.Rolodex{
		SchemaVersion: 1, ID: "01HB00000000000000000000AA", Slug: "a", Label: "A",
		Visibility: model.VisibilityBundled,
		Entries: []model.Entry{{
			NodeCore: model.NodeCore{ID: "01HB00000000000000000000A1", Slug: "to-b", Label: "to b"},
			Kind:     model.KindPointer,
			Pointer:  &model.PointerPayload{To: "01HB00000000000000000000BB"},
		}},
	}
	b := model.Rolodex{
		SchemaVersion: 1, ID: "01HB00000000000000000000BB", Slug: "b", Label: "B",
		Visibility: model.VisibilityBundled,
		Entries: []model.Entry{{
			NodeCore: model.NodeCore{ID: "01HB00000000000000000000B1", Slug: "to-a", Label: "to a"},
			Kind:     model.KindPointer,
			Pointer:  &model.PointerPayload{To: "01HB00000000000000000000AA"},
		}},
	}
	root := mergedRoot(model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000R1", Slug: "a", Label: "A"},
		Kind:     model.KindPointer,
		Pointer:  &model.PointerPayload{To: a.ID},
	})
	r := &fakeResolver{rolodexes: map[string]model.Rolodex{a.ID: a, b.ID: b}}

	// /a/to-b/to-a/to-b/to-a/... — keeps alternating; should error at the cap.
	deep := "/a" + strings.Repeat("/to-b/to-a", 20)
	_, err := path.Resolve(r, root, deep)
	if !errors.Is(err, path.ErrCycle) {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
}
