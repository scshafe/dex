package session_test

import (
	"testing"
	"time"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/path"
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

func TestApplyDoesNotMutateCallerState(t *testing.T) {
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
	// Pre-seed mutable state so we have something to observe.
	st.Resolved["seed"] = "value"
	st.PendingConcerns = append(st.PendingConcerns,
		session.PendingConcern{LocalID: "k"})
	// Make sure PreviousCursors has spare capacity — that's the case
	// where the slice header alias would let next overwrite st.
	st.PreviousCursors = make([]session.Cursor, 0, 4)

	snapResolved := map[string]string{}
	for k, v := range st.Resolved {
		snapResolved[k] = v
	}
	snapPending := append([]session.PendingConcern(nil), st.PendingConcerns...)
	snapPrev := append([]session.Cursor(nil), st.PreviousCursors...)

	next, _, err := session.Apply(r, st, session.Action{
		Type:   session.ActionDrill,
		Target: tools.ID,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Mutate the post-action state in every reference-typed field.
	next.Resolved["seed"] = "MUTATED"
	next.Resolved["new"] = "added"
	next.PendingConcerns[0].LocalID = "MUTATED"
	if len(next.PreviousCursors) > 0 {
		next.PreviousCursors[0].RolodexID = "MUTATED"
	}

	// Caller's state must still look exactly like the pre-call snapshot.
	if got, want := st.Resolved, snapResolved; !mapsEqual(got, want) {
		t.Fatalf("caller's Resolved was mutated: got %+v want %+v", got, want)
	}
	if len(st.PendingConcerns) != len(snapPending) ||
		st.PendingConcerns[0].LocalID != snapPending[0].LocalID {
		t.Fatalf("caller's PendingConcerns was mutated: got %+v want %+v",
			st.PendingConcerns, snapPending)
	}
	if len(st.PreviousCursors) != len(snapPrev) {
		t.Fatalf("caller's PreviousCursors length changed: got %d want %d",
			len(st.PreviousCursors), len(snapPrev))
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestDrillByPathToPointer(t *testing.T) {
	tools := model.Rolodex{
		SchemaVersion: 1, ID: "01HB00000000000000000000T1", Slug: "tools", Label: "Tools",
		Visibility: model.VisibilityBundled,
	}
	root := model.Rolodex{
		Slug: "merged-root",
		Entries: []model.Entry{{
			NodeCore: model.NodeCore{ID: "01HB00000000000000000000E1", Slug: "tools", Label: "Tools"},
			Kind:     model.KindPointer,
			Pointer:  &model.PointerPayload{To: tools.ID},
		}},
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{tools.ID: tools},
		root:      root,
	}
	st := newState(t)
	st2, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionDrill, Target: "/tools",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	// Drilling into a pointer entry advances the cursor to its target
	// rolodex (browse mode), and stamps the display path.
	if st2.Cursor.RolodexID != tools.ID {
		t.Fatalf("cursor.rolodex_id: got %q want %q", st2.Cursor.RolodexID, tools.ID)
	}
	if st2.Cursor.Mode != session.CursorModeBrowse {
		t.Fatalf("cursor.mode: got %q want browse", st2.Cursor.Mode)
	}
	if st2.Cursor.Path != "/tools" {
		t.Fatalf("cursor.path: got %q want /tools", st2.Cursor.Path)
	}
}

func TestDrillByPathToInfoEntry(t *testing.T) {
	root := model.Rolodex{
		ID:   "01HB00000000000000000000R1",
		Slug: "merged-root",
		Entries: []model.Entry{{
			NodeCore: model.NodeCore{ID: "01HB00000000000000000000E2", Slug: "readme", Label: "Readme"},
			Kind:     model.KindInfo,
			Info:     &model.InfoPayload{Content: "hi"},
		}},
	}
	r := &fakeResolver{root: root}
	st := newState(t)
	st2, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionDrill, Target: "/readme",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	if st2.Cursor.EntryID != "01HB00000000000000000000E2" {
		t.Fatalf("cursor.entry_id: got %q", st2.Cursor.EntryID)
	}
	if st2.Cursor.Mode != session.CursorModeEntry {
		t.Fatalf("cursor.mode: got %q want entry", st2.Cursor.Mode)
	}
}

func TestDrillByPathNotFound(t *testing.T) {
	r := &fakeResolver{root: model.Rolodex{Slug: "merged-root"}}
	st := newState(t)
	_, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionDrill, Target: "/missing",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	// path package errors map to NOT_FOUND in the envelope.
	if env.Error.Code != session.ErrNotFound {
		t.Fatalf("expected NOT_FOUND, got %s", env.Error.Code)
	}
	// Suppress the unused-import lint on `path` until other tasks
	// reference it directly.
	_ = path.ErrNotFound
}

func TestPopRestoresPreviousCursor(t *testing.T) {
	tools := model.Rolodex{
		SchemaVersion: 1, ID: "01HB00000000000000000000T1", Slug: "tools", Label: "Tools",
		Visibility: model.VisibilityBundled,
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{tools.ID: tools},
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st, _, _ = session.Apply(r, st, session.Action{Type: session.ActionDrill, Target: tools.ID})
	if st.Cursor.RolodexID != tools.ID {
		t.Fatalf("setup: drill failed, cursor=%+v", st.Cursor)
	}

	st2, env, err := session.Apply(r, st, session.Action{Type: session.ActionPop})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	if st2.Cursor.RolodexID != "" {
		t.Fatalf("cursor.rolodex_id after pop: got %q want empty", st2.Cursor.RolodexID)
	}
	if len(st2.PreviousCursors) != 0 {
		t.Fatalf("previous_cursors after pop: got %d want 0", len(st2.PreviousCursors))
	}
}

func TestPopOnEmptyStackIsNoop(t *testing.T) {
	r := &fakeResolver{root: model.Rolodex{Slug: "merged-root"}}
	st := newState(t)
	st2, env, err := session.Apply(r, st, session.Action{Type: session.ActionPop})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	// Version still advances — pop is a step.
	if st2.Version != st.Version+1 {
		t.Fatalf("version: got %d want %d", st2.Version, st.Version+1)
	}
}

func TestActivateInfoContentEmitsStdout(t *testing.T) {
	parent := model.Rolodex{ID: "01HB00000000000000000000P1", Slug: "p"}
	info := model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000I1", Slug: "readme", Label: "R"},
		Kind:     model.KindInfo,
		Info:     &model.InfoPayload{Content: "hello"},
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{parent.ID: parent},
		entries:   map[string]entryHit{info.ID: {entry: info, parent: parent}},
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st.Cursor = session.Cursor{
		RolodexID: parent.ID, EntryID: info.ID, Mode: session.CursorModeEntry,
	}
	_, env, err := session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	if len(env.Effects) != 1 || env.Effects[0].Type != session.EffectStdout {
		t.Fatalf("effects: got %+v want one stdout", env.Effects)
	}
	if env.Effects[0].Content != "hello" {
		t.Fatalf("stdout content: got %q want hello", env.Effects[0].Content)
	}
}

func TestActivateRequiresEntryUnderCursor(t *testing.T) {
	r := &fakeResolver{root: model.Rolodex{Slug: "merged-root"}}
	st := newState(t)
	// Cursor is in browse mode; no entry to activate.
	_, env, err := session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error.Code != session.ErrInvalidTarget {
		t.Fatalf("error.code: got %s want INVALID_TARGET", env.Error.Code)
	}
}

func TestActivatePointerDrillsIntoTarget(t *testing.T) {
	target := model.Rolodex{ID: "01HB00000000000000000000T2", Slug: "t2"}
	parent := model.Rolodex{ID: "01HB00000000000000000000P1", Slug: "p"}
	ptr := model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000I2", Slug: "go", Label: "go"},
		Kind:     model.KindPointer,
		Pointer:  &model.PointerPayload{To: target.ID},
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{target.ID: target, parent.ID: parent},
		entries:   map[string]entryHit{ptr.ID: {entry: ptr, parent: parent}},
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st.Cursor = session.Cursor{RolodexID: parent.ID, EntryID: ptr.ID, Mode: session.CursorModeEntry}

	st2, env, err := session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	if st2.Cursor.RolodexID != target.ID {
		t.Fatalf("cursor.rolodex_id: got %q want %q", st2.Cursor.RolodexID, target.ID)
	}
	if st2.Cursor.Mode != session.CursorModeBrowse {
		t.Fatalf("cursor.mode: got %q want browse", st2.Cursor.Mode)
	}
	if len(env.Effects) != 0 {
		t.Fatalf("effects: got %+v want none", env.Effects)
	}
}

func TestActivateCommandAllConcernsSatisfiedEmitsSpawn(t *testing.T) {
	parent := model.Rolodex{ID: "01HB00000000000000000000P1", Slug: "p"}
	cmd := model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000C1", Slug: "deploy", Label: "Deploy"},
		Kind:     model.KindCommand,
		Command: &model.CommandPayload{
			Template: "deploy --ns {ns} --tag {tag}",
			Concerns: []model.Concern{
				{NodeCore: model.NodeCore{ID: "01HB00000000000000000000K1"}, LocalID: "ns", Required: true},
				{NodeCore: model.NodeCore{ID: "01HB00000000000000000000K2"}, LocalID: "tag", Default: "latest"},
			},
		},
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{parent.ID: parent},
		entries:   map[string]entryHit{cmd.ID: {entry: cmd, parent: parent}},
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st.Cursor = session.Cursor{RolodexID: parent.ID, EntryID: cmd.ID, Mode: session.CursorModeEntry}
	st.Resolved = map[string]string{"ns": "prod"}

	_, env, err := session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	if len(env.Effects) != 1 || env.Effects[0].Type != session.EffectSpawn {
		t.Fatalf("effects: got %+v want one spawn", env.Effects)
	}
	want := "deploy --ns prod --tag latest"
	if env.Effects[0].ShellCommand != want {
		t.Fatalf("shell_command: got %q want %q", env.Effects[0].ShellCommand, want)
	}
}
