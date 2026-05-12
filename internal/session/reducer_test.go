package session_test

import (
	"testing"
	"time"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/session"
)

// fakeResolver is an in-memory Resolver for reducer tests. Mirrors
// the pattern from internal/path/path_test.go.
type fakeResolver struct {
	rolodexes map[string]model.Rolodex
	entries   map[string]entryHit // entry_id → entry + parent rolodex
	root      model.Rolodex
}

type entryHit struct {
	entry  model.Entry
	parent model.Rolodex
}

func (f *fakeResolver) LookupByID(id string) (model.Rolodex, bool, error) {
	r, ok := f.rolodexes[id]
	return r, ok, nil
}

func (f *fakeResolver) LookupEntryByID(id string) (model.Entry, model.Rolodex, bool, error) {
	h, ok := f.entries[id]
	if !ok {
		return model.Entry{}, model.Rolodex{}, false, nil
	}
	return h.entry, h.parent, true, nil
}

func (f *fakeResolver) MergedRoot() (model.Rolodex, error) {
	return f.root, nil
}

func newState(t *testing.T) session.State {
	t.Helper()
	now := time.Now()
	return session.State{
		ID:              "ses_TESTID",
		Cursor:          session.Cursor{Mode: session.CursorModeBrowse},
		Resolved:        map[string]string{},
		PendingConcerns: []session.PendingConcern{},
		Version:         0,
		CreatedAt:       now,
		LastTouched:     now,
		PreviousCursors: []session.Cursor{},
	}
}

func TestDrillByUUID(t *testing.T) {
	tools := model.Rolodex{
		SchemaVersion: 1,
		ID:            "01HB00000000000000000000T1",
		Slug:          "tools",
		Label:         "Tools",
		Visibility:    model.VisibilityBundled,
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{tools.ID: tools},
		entries:   map[string]entryHit{},
		root:      model.Rolodex{Slug: "merged-root"},
	}

	st := newState(t)
	st2, env, err := session.Apply(r, st, session.Action{
		Type:   session.ActionDrill,
		Target: tools.ID,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got envelope %+v", env)
	}
	if st2.Cursor.RolodexID != tools.ID {
		t.Fatalf("cursor.rolodex_id: got %q want %q", st2.Cursor.RolodexID, tools.ID)
	}
	if st2.Cursor.Mode != session.CursorModeBrowse {
		t.Fatalf("cursor.mode: got %q want browse", st2.Cursor.Mode)
	}
	if st2.Version != 1 {
		t.Fatalf("version: got %d want 1", st2.Version)
	}
	if env.Session.ID != st2.ID {
		t.Fatalf("envelope.session.id: got %q want %q", env.Session.ID, st2.ID)
	}
}

func TestDrillByUUIDNotFound(t *testing.T) {
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{},
		entries:   map[string]entryHit{},
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st2, env, err := session.Apply(r, st, session.Action{
		Type:   session.ActionDrill,
		Target: "01HB00000000000000000000XX",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false, got %+v", env)
	}
	if env.Error == nil || env.Error.Code != session.ErrInvalidTarget {
		t.Fatalf("expected INVALID_TARGET, got %+v", env.Error)
	}
	if st2.Version != st.Version {
		t.Fatalf("failed action must not bump version (got %d, want %d)", st2.Version, st.Version)
	}
}
