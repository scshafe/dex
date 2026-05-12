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

func TestActivateCommandStagesPendingConcerns(t *testing.T) {
	parent := model.Rolodex{ID: "01HB00000000000000000000P1", Slug: "p"}
	cmd := model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000C2", Slug: "deploy", Label: "Deploy"},
		Kind:     model.KindCommand,
		Command: &model.CommandPayload{
			Template: "deploy --ns {ns}",
			Concerns: []model.Concern{
				{NodeCore: model.NodeCore{ID: "01HB00000000000000000000K1"}, LocalID: "ns", Required: true},
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

	st2, env, err := session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error.Code != session.ErrUnresolvedRequired {
		t.Fatalf("error.code: got %s want UNRESOLVED_REQUIRED", env.Error.Code)
	}
	if env.Error.Concern != "ns" {
		t.Fatalf("error.concern: got %q want ns", env.Error.Concern)
	}
	if len(st2.PendingConcerns) != 1 || st2.PendingConcerns[0].LocalID != "ns" {
		t.Fatalf("state.pending_concerns: got %+v", st2.PendingConcerns)
	}
}

func TestResolveSetsValueAndShrinksPending(t *testing.T) {
	st := newState(t)
	st.PendingConcerns = []session.PendingConcern{
		{LocalID: "ns", Required: true},
		{LocalID: "tag"},
	}
	r := &fakeResolver{root: model.Rolodex{Slug: "merged-root"}}

	st2, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionResolve, Concern: "ns", Value: "prod",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope: %+v", env)
	}
	if got := st2.Resolved["ns"]; got != "prod" {
		t.Fatalf("resolved[ns]: got %q want prod", got)
	}
	if len(st2.PendingConcerns) != 1 || st2.PendingConcerns[0].LocalID != "tag" {
		t.Fatalf("pending: got %+v want [tag]", st2.PendingConcerns)
	}
}

func TestResolveUnknownConcernErrors(t *testing.T) {
	st := newState(t)
	st.PendingConcerns = []session.PendingConcern{{LocalID: "ns", Required: true}}
	r := &fakeResolver{root: model.Rolodex{Slug: "merged-root"}}

	_, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionResolve, Concern: "missing", Value: "x",
	})
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

func TestEndToEnd_DrillResolveActivate(t *testing.T) {
	// commands rolodex: contains a deploy command with one required concern.
	cmd := model.Entry{
		NodeCore: model.NodeCore{ID: "01HB000000000000000000CMD", Slug: "deploy", Label: "Deploy"},
		Kind:     model.KindCommand,
		Command: &model.CommandPayload{
			Template: "kubectl apply -n {ns} -f svc.yaml",
			Concerns: []model.Concern{
				{NodeCore: model.NodeCore{ID: "01HB00000000000000000000K1"}, LocalID: "ns", Required: true},
			},
		},
	}
	commands := model.Rolodex{
		SchemaVersion: 1, ID: "01HB000000000000000000CMS", Slug: "commands",
		Visibility: model.VisibilityBundled,
		Entries:    []model.Entry{cmd},
	}
	root := model.Rolodex{
		Slug: "merged-root",
		Entries: []model.Entry{{
			NodeCore: model.NodeCore{ID: "01HB000000000000000000RTE", Slug: "commands"},
			Kind:     model.KindPointer,
			Pointer:  &model.PointerPayload{To: commands.ID},
		}},
	}
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{commands.ID: commands},
		entries:   map[string]entryHit{cmd.ID: {entry: cmd, parent: commands}},
		root:      root,
	}

	st := newState(t)

	// 1. Drill into /commands.
	st, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionDrill, Target: "/commands",
	})
	if err != nil || !env.OK {
		t.Fatalf("drill /commands: err=%v env=%+v", err, env)
	}

	// 2. Drill into the deploy entry by uuid (path-based for entries
	//    inside a rolodex isn't reachable from the merged root in this
	//    fake; uuid drill is the equivalent step).
	st, env, err = session.Apply(r, st, session.Action{
		Type: session.ActionDrill, Target: cmd.ID,
	})
	// Drilling into a non-rolodex uuid currently errors (drillByUUID
	// only knows rolodexes). For the integration test, set the cursor
	// directly to simulate "the user is now on the deploy entry."
	if env.OK {
		t.Fatalf("drill into entry uuid should error in v1 (rolodex-only); got ok=true")
	}
	st.Cursor = session.Cursor{RolodexID: commands.ID, EntryID: cmd.ID, Mode: session.CursorModeEntry}

	// 3. First activate: stages pending concern, errors UNRESOLVED_REQUIRED.
	st, env, err = session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("activate (first): %v", err)
	}
	if env.OK {
		t.Fatalf("first activate must stage pending and error")
	}
	if env.Error.Code != session.ErrUnresolvedRequired || env.Error.Concern != "ns" {
		t.Fatalf("first activate error: %+v", env.Error)
	}

	// 4. Resolve ns=prod.
	st, env, err = session.Apply(r, st, session.Action{
		Type: session.ActionResolve, Concern: "ns", Value: "prod",
	})
	if err != nil || !env.OK {
		t.Fatalf("resolve: err=%v env=%+v", err, env)
	}

	// 5. Second activate: emits spawn.
	_, env, err = session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil || !env.OK {
		t.Fatalf("activate (final): err=%v env=%+v", err, env)
	}
	if len(env.Effects) != 1 || env.Effects[0].Type != session.EffectSpawn {
		t.Fatalf("effects: got %+v", env.Effects)
	}
	want := "kubectl apply -n prod -f svc.yaml"
	if env.Effects[0].ShellCommand != want {
		t.Fatalf("shell_command: got %q want %q", env.Effects[0].ShellCommand, want)
	}
}

func TestStaleSessionCaughtAtDispatchForPop(t *testing.T) {
	// The cursor points at an entry the resolver no longer knows about.
	// Even a non-activate action (pop) should refuse to advance.
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{},
		entries:   map[string]entryHit{}, // cursor.entry_id is unknown
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st.Cursor = session.Cursor{
		RolodexID: "01HB00000000000000000000P1",
		EntryID:   "01HB00000000000000000000XX", // not in resolver.entries
		Mode:      session.CursorModeEntry,
	}

	st2, env, err := session.Apply(r, st, session.Action{Type: session.ActionPop})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false on pop with stale cursor")
	}
	if env.Error.Code != session.ErrStaleSession {
		t.Fatalf("error.code: got %s want STALE_SESSION", env.Error.Code)
	}
	if st2.Version != st.Version {
		t.Fatalf("version: got %d want %d (no advance on stale)", st2.Version, st.Version)
	}
}

func TestStaleSessionCaughtAtDispatchForResolve(t *testing.T) {
	r := &fakeResolver{
		rolodexes: map[string]model.Rolodex{},
		entries:   map[string]entryHit{},
		root:      model.Rolodex{Slug: "merged-root"},
	}
	st := newState(t)
	st.Cursor = session.Cursor{
		RolodexID: "01HB00000000000000000000P1",
		EntryID:   "01HB00000000000000000000XX",
		Mode:      session.CursorModeEntry,
	}
	st.PendingConcerns = []session.PendingConcern{{LocalID: "ns", Required: true}}

	_, env, err := session.Apply(r, st, session.Action{
		Type: session.ActionResolve, Concern: "ns", Value: "prod",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false on resolve with stale cursor")
	}
	if env.Error.Code != session.ErrStaleSession {
		t.Fatalf("error.code: got %s want STALE_SESSION", env.Error.Code)
	}
}

func TestActivateCommandAllOptionalNoDefaultsErrors(t *testing.T) {
	// Optional concerns with no default are still surfaced as pending
	// because their {local_id} would substitute to empty string,
	// which is rarely what the user wants. The architect treats this
	// as a v1 hard error; relax later if needed.
	parent := model.Rolodex{ID: "01HB00000000000000000000P1", Slug: "p"}
	cmd := model.Entry{
		NodeCore: model.NodeCore{ID: "01HB00000000000000000000C3", Slug: "x"},
		Kind:     model.KindCommand,
		Command: &model.CommandPayload{
			Template: "x --tag {tag}",
			Concerns: []model.Concern{
				{NodeCore: model.NodeCore{ID: "01HB00000000000000000000K1"}, LocalID: "tag" /* not required, no default */},
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

	_, env, err := session.Apply(r, st, session.Action{Type: session.ActionActivate})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error.Code != session.ErrUnresolvedRequired {
		t.Fatalf("error.code: got %s", env.Error.Code)
	}
}

func TestEnvelopeOfMirrorsState(t *testing.T) {
	st := newState(t)
	st.Cursor = session.Cursor{RolodexID: "rdx-1", Mode: session.CursorModeBrowse}
	st.Version = 5
	st.Resolved = map[string]string{"k": "v"}

	env := session.EnvelopeOf(st)
	if !env.OK {
		t.Fatalf("EnvelopeOf should always return ok=true")
	}
	if env.Session.ID != st.ID {
		t.Fatalf("session.id: got %q want %q", env.Session.ID, st.ID)
	}
	if env.Session.Cursor.RolodexID != "rdx-1" {
		t.Fatalf("cursor.rolodex_id: got %q", env.Session.Cursor.RolodexID)
	}
	if env.Session.Version != 5 {
		t.Fatalf("version: got %d want 5", env.Session.Version)
	}
	if env.Session.Resolved["k"] != "v" {
		t.Fatalf("resolved[k]: got %q", env.Session.Resolved["k"])
	}
	if env.Error != nil {
		t.Fatalf("error should be nil, got %+v", env.Error)
	}
	if len(env.Effects) != 0 {
		t.Fatalf("effects should be empty, got %+v", env.Effects)
	}
}
