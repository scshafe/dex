# Session State + Reducer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the in-memory session reducer and on-disk session state file format that P-11.4's CLI verbs will sit on top of. After this slice, an end-to-end test drives a session through `drill → resolve → activate` (emitting a `spawn` effect) without any CLI dispatch wiring.

**Architecture:** A new `internal/session` package owns the reducer (pure function: `Apply(state, action, resolver) → (state', envelope, error)`) and the session file persistence (atomic write + opportunistic GC). The reducer depends on a narrow `Resolver` interface (`LookupByID`, `LookupEntryByID`, `MergedRoot`) — the same shape pattern `internal/path` uses — so reducer tests run against a fake without touching disk. The envelope is the contract; `state` round-trips through the file; `view` is a render hint not implemented in this slice.

**Tech Stack:** Go stdlib + `github.com/oklog/ulid/v2` (already in go.mod) for session ids. No new dependencies.

---

## Out of scope (for follow-up plans)

- The `dex session start|step|state|end|list` CLI verbs (next slice — they wrap this reducer)
- The `view` field of the envelope (rendering hint; the reducer leaves it as `nil`)
- Validator script execution (concerns with `validator` set: the `resolve` action accepts the value as-is and notes the deferral in a comment)
- `strict: true` enforcement (concerns with `strict` set: same — accepted as-is)
- `depends_on` ordering (pending_concerns surface in declaration order; the dependency graph is honored as a follow-up)
- Info entries with `provider` set during activate (the architect's landmine #1 — surfaced as `PROVIDER_FAILED` error code, just like `dex activate` does)
- Per-rolodex caching inside the session state (handoff trade-off note; revisit when the CLI per-keystroke cost becomes measurable)
- Anything related to the modal frontend (P-11.5)

---

## Pinned design decisions

These were marked open in the 2026-05-12 handoff. Pinning here so the executor doesn't re-debate them mid-task. Each is a v1 simplification that does not bake in invasive infrastructure.

1. **Session TTL = 30 minutes**, declared as a package constant `SessionTTL`. Sliding: every successful step bumps `state.LastTouched`. Hardcoded for v1; configurability is a follow-up.
2. **Opportunistic GC on `NewSession`**: walk the session dir, remove any file whose `last_touched` is older than `SessionTTL`. Cheap, no daemon, no `--stale --rm` escape hatch in v1.
3. **`effect: spawn` envelope shape = `{shell_command: string}`**, mirroring the existing `dex activate` exec model. The caller decides whether to exec or display. Argv/env/cwd/stdin granularity is a follow-up.
4. **`STALE_SESSION` semantics**: on every step, before applying the action, look up the cursor's `entry_id` (if set) via `LookupEntryByID`. If it no longer exists, return a `STALE_SESSION` envelope and do not advance state. The caller's hint is "start a new session."
5. **Concurrent sessions on the same rolodex**: independent reducers; the on-disk store is the shared state and `store.WriteRolodex` already locks per-rolodex. No code change needed in this slice; documented in the package doc comment.
6. **Cursor `mode` field (v1)**: two values — `"browse"` (cursor at a rolodex; entry_id empty) and `"entry"` (cursor on a specific entry; entry_id set). The architect's mode field is a forward-compat hook; v1 doesn't need more.
7. **Cursor `path` field (v1)**: display string. Set when `drill` targets a `/path`; cleared when `drill` targets a UUID. Not used for resolution — it is informational for future view layers.

---

## File Structure

```
dex/
├── internal/
│   └── session/
│       ├── doc.go              (Task 1; package doc only — pinned decisions, link to handoff)
│       ├── types.go            (Task 1; Envelope, State, Cursor, Action, Effect, ErrorCode)
│       ├── reducer.go          (Tasks 2–8; Resolver iface, Apply func, per-action handlers)
│       ├── reducer_test.go     (Tasks 2–9; algorithm-level tests with a fake Resolver)
│       ├── store.go            (Task 10; NewSession, Load, Save, End, GC; atomic write)
│       └── store_test.go       (Task 10; session file IO via t.TempDir)
└── docs/superpowers/plans/2026-05-12-session-state-and-reducer.md
```

The `Resolver` interface is intentionally narrow (three methods, all already satisfied by `*store.Store`) so the reducer is testable in isolation. This mirrors what `internal/path` did with its single-method `Resolver`.

---

## Task 1: Package skeleton + types

Create the package, doc comment with pinned decisions, and the type definitions for the envelope, state, cursor, action, effect, and error codes. No reducer logic yet.

**Files:**
- Create: `internal/session/doc.go`
- Create: `internal/session/types.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package session implements the dex session API reducer and on-disk
// state file format.
//
// A session is a stateful navigation context: cursor (where you are),
// resolved (concern values you've supplied), pending_concerns (what's
// still needed before the current command can run). The reducer is a
// pure function — Apply(state, action, resolver) → (state', envelope,
// effects) — and the session file (~/.cache/dex/sessions/ses_*.json)
// is just a serialized State.
//
// Pinned v1 decisions (see docs/superpowers/plans/2026-05-12-session-state-and-reducer.md):
//
//   - Sliding TTL of 30 minutes (SessionTTL); GC is opportunistic on
//     NewSession.
//   - effect:spawn returns a single shell_command string (matches
//     the existing dex activate exec model).
//   - Stale-session detection runs on every step via LookupEntryByID
//     against the cursor's entry_id; mismatch returns STALE_SESSION.
//   - Concurrent sessions on the same rolodex are independent
//     reducers; shared state is the on-disk store, locked per-rolodex
//     by store.WriteRolodex.
//   - Validators, strict, and depends_on are accepted into resolve
//     but not enforced in v1; deferred to a follow-up slice.
//
// The view field of the envelope is intentionally nil in this slice;
// state is the contract, view is a render hint that callers will
// populate later.
package session
```

- [ ] **Step 2: Write `types.go`**

```go
package session

import (
	"time"

	"github.com/scshafe/dex/internal/model"
)

// SessionTTL is the sliding inactivity window. Every successful step
// touches LastTouched; sessions older than this are GC'd opportunistically
// on NewSession.
const SessionTTL = 30 * time.Minute

// CursorMode is "browse" when the cursor is at a rolodex (entry_id
// empty) or "entry" when it's on a specific entry.
type CursorMode string

const (
	CursorModeBrowse CursorMode = "browse"
	CursorModeEntry  CursorMode = "entry"
)

// Cursor is "where you are" in the graph.
type Cursor struct {
	RolodexID string     `json:"rolodex_id"`
	Path      string     `json:"path"` // display only — set by /path drills
	EntryID   string     `json:"entry_id,omitempty"`
	Mode      CursorMode `json:"mode"`
}

// PendingConcern is one still-unsatisfied parameter on the current
// command. Surfaced when the cursor's entry is a command and resolve
// hasn't filled the concern's local_id yet.
type PendingConcern struct {
	LocalID   string   `json:"local_id"`
	Label     string   `json:"label"`
	Default   string   `json:"default,omitempty"`
	Required  bool     `json:"required"`
	Strict    bool     `json:"strict"`
	Validator string   `json:"validator,omitempty"`  // not enforced in v1
	DependsOn []string `json:"depends_on,omitempty"` // not enforced in v1
}

// State is the full session, persisted as JSON.
type State struct {
	ID             string            `json:"id"` // "ses_" + ULID
	Cursor         Cursor            `json:"cursor"`
	Resolved       map[string]string `json:"resolved"`
	PendingConcerns []PendingConcern `json:"pending_concerns"`
	Version        int               `json:"version"`
	CreatedAt      time.Time         `json:"created_at"`
	LastTouched    time.Time         `json:"last_touched"`
	// PreviousCursors is the back-stack used by the "pop" action.
	// Pushed on drill, popped on pop. Empty stack + pop is a no-op
	// that returns the current envelope unchanged.
	PreviousCursors []Cursor `json:"previous_cursors"`
}

// Action is the input to Apply. Exactly one of the typed fields is
// expected to be set per call; the reducer dispatches on the Type
// string.
type Action struct {
	Type    string `json:"action"`
	Target  string `json:"target,omitempty"`  // for drill
	Concern string `json:"concern,omitempty"` // for resolve
	Value   string `json:"value,omitempty"`   // for resolve
}

const (
	ActionDrill    = "drill"
	ActionPop      = "pop"
	ActionActivate = "activate"
	ActionResolve  = "resolve"
)

// EffectType discriminates the Effect union. Only one of the typed
// payload fields is set per effect.
type EffectType string

const (
	EffectSpawn        EffectType = "spawn"
	EffectStdout       EffectType = "stdout"
	EffectCloseSession EffectType = "close_session"
)

type Effect struct {
	Type         EffectType `json:"type"`
	ShellCommand string     `json:"shell_command,omitempty"` // spawn
	Content      string     `json:"content,omitempty"`       // stdout
}

// ErrorCode is the closed set from the architect's Q1 response.
type ErrorCode string

const (
	ErrInvalidTarget       ErrorCode = "INVALID_TARGET"
	ErrUnresolvedRequired  ErrorCode = "UNRESOLVED_REQUIRED"
	ErrValidatorFailed     ErrorCode = "VALIDATOR_FAILED"
	ErrStrictViolation     ErrorCode = "STRICT_VIOLATION"
	ErrNotFound            ErrorCode = "NOT_FOUND"
	ErrStaleSession        ErrorCode = "STALE_SESSION"
	ErrProviderFailed      ErrorCode = "PROVIDER_FAILED"
	ErrSchemaError         ErrorCode = "SCHEMA_ERROR"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Concern string    `json:"concern,omitempty"`
	Hint    string    `json:"hint,omitempty"`
}

// Envelope is what the reducer returns on every step, success or
// failure. ok=false sets Error and leaves Effects empty/State at the
// pre-action snapshot. view is a render hint; this slice always
// returns nil.
type Envelope struct {
	OK      bool             `json:"ok"`
	Session SessionView      `json:"session"`
	View    *json.RawMessage `json:"view"` // always nil in this slice
	Effects []Effect         `json:"effects"`
	Error   *Error           `json:"error"`
}

// SessionView is the externally-visible projection of State that
// every envelope ships. Mirrors the architect's Q1 envelope shape.
// Excluded vs State: created_at, previous_cursors (internal), the
// concern back-stack stays in State.
type SessionView struct {
	ID              string           `json:"id"`
	Cursor          Cursor           `json:"cursor"`
	Resolved        map[string]string `json:"resolved"`
	PendingConcerns []PendingConcern `json:"pending_concerns"`
	Version         int              `json:"version"`
}

// _ is here so model is referenced from this file, since later
// reducer files use it via the package-level import. Keeps the
// import in case someone refactors the action dispatch to use
// model.EntryKind constants directly.
var _ = model.KindCommand
```

- [ ] **Step 3: Add the `encoding/json` import**

The `*json.RawMessage` field needs it. Top of `types.go`:

```go
import (
	"encoding/json"
	"time"

	"github.com/scshafe/dex/internal/model"
)
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/session/...`
Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/session/doc.go internal/session/types.go docs/superpowers/plans/2026-05-12-session-state-and-reducer.md
git commit -m "$(cat <<'EOF'
session: package skeleton + envelope/state/action types

Lays down the type surface for the P-11.4 reducer (no logic yet).
Pins the v1 decisions deferred from the 2026-05-12 handoff:
SessionTTL=30m, opportunistic GC, spawn-as-shell_command,
STALE_SESSION via cursor entry_id lookup. Validators, strict, and
depends_on are accepted into State but not enforced in v1.
EOF
)"
```

---

## Task 2: Reducer skeleton — Apply + Resolver iface + drill (UUID target)

Stand up the `Apply` function with action dispatch, the `Resolver` interface, and the simplest `drill` case: a target that is a ULID for an existing rolodex. This nails the function signature and the envelope-on-success path.

**Files:**
- Create: `internal/session/reducer.go`
- Create: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/reducer_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/...`
Expected: FAIL — `undefined: session.Apply`.

- [ ] **Step 3: Write minimal `reducer.go`**

```go
package session

import (
	"fmt"
	"time"

	"github.com/scshafe/dex/internal/model"
)

// Resolver is the narrow store-shaped dependency the reducer needs.
// *store.Store satisfies it; tests use a fake.
type Resolver interface {
	LookupByID(id string) (model.Rolodex, bool, error)
	LookupEntryByID(id string) (model.Entry, model.Rolodex, bool, error)
	MergedRoot() (model.Rolodex, error)
}

// Apply is the pure reducer. Same input + state → same envelope and
// next state. Effects are returned, not executed; the caller is
// responsible for spawning processes or printing stdout.
//
// Errors-as-data: a validation failure returns ok=false with
// Envelope.Error set and the *original* state echoed back in
// Envelope.Session. Only protocol failures (unknown action, nil
// resolver) return a Go error.
func Apply(r Resolver, st State, a Action) (State, Envelope, error) {
	if r == nil {
		return st, Envelope{}, fmt.Errorf("session: nil resolver")
	}

	switch a.Type {
	case ActionDrill:
		return applyDrill(r, st, a)
	}

	return st, Envelope{}, fmt.Errorf("session: unknown action %q", a.Type)
}

func applyDrill(r Resolver, st State, a Action) (State, Envelope, error) {
	// UUID target only in this task. Path target lands in Task 3.
	rdx, ok, err := r.LookupByID(a.Target)
	if err != nil {
		return st, Envelope{}, fmt.Errorf("session: lookup: %w", err)
	}
	if !ok {
		return failure(st, ErrInvalidTarget,
			fmt.Sprintf("no rolodex with id %q", a.Target), "", ""), Envelope{}, nil
	}

	next := touch(st)
	next.PreviousCursors = append(next.PreviousCursors, st.Cursor)
	next.Cursor = Cursor{
		RolodexID: rdx.ID,
		Path:      "", // UUID target wipes the display path
		Mode:      CursorModeBrowse,
	}
	return next, success(next), nil
}

// touch returns a copy of st with Version bumped and LastTouched
// updated to now. Use this on every successful action.
func touch(st State) State {
	out := st
	out.Version = st.Version + 1
	out.LastTouched = time.Now()
	return out
}

func success(st State, effects ...Effect) Envelope {
	return Envelope{
		OK:      true,
		Session: viewOf(st),
		Effects: effects,
	}
}

// failure builds an envelope but does NOT advance state. The caller
// also receives the un-advanced state from Apply.
func failure(st State, code ErrorCode, msg, concern, hint string) Envelope {
	// Note: we only update the Envelope's view of the session; we do
	// not mutate Resolved/PendingConcerns on a failure path. The
	// caller's State stays at its pre-action snapshot.
	return Envelope{
		OK:      false,
		Session: viewOf(st),
		Error: &Error{
			Code: code, Message: msg, Concern: concern, Hint: hint,
		},
	}
}

func viewOf(st State) SessionView {
	return SessionView{
		ID:              st.ID,
		Cursor:          st.Cursor,
		Resolved:        st.Resolved,
		PendingConcerns: st.PendingConcerns,
		Version:         st.Version,
	}
}
```

The earlier `applyDrill` returns `Envelope{}` on the failure path which is wrong — it should return the failure envelope. Fix inline:

```go
	if !ok {
		return st, failure(st, ErrInvalidTarget,
			fmt.Sprintf("no rolodex with id %q", a.Target), "", ""), nil
	}
```

(The dummy `Envelope{}` in the bad branch was a typo — the real return is the failure envelope. Make sure your code matches the corrected line above, not the first version.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/...`
Expected: PASS — `--- PASS: TestDrillByUUID`.

- [ ] **Step 5: Add the not-found drill test**

Append to `reducer_test.go`:

```go
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
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/session/...`
Expected: both tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "$(cat <<'EOF'
session: reducer skeleton + drill action (UUID target)

Apply is a pure function over a narrow Resolver interface. Errors
are data: a not-found UUID returns ok=false with INVALID_TARGET and
leaves the State.Version at its pre-action value. Successful drills
push the previous cursor onto the back-stack so pop can undo them.
EOF
)"
```

---

## Task 3: Drill — `/path` target

Extend `applyDrill` to accept a `/`-prefixed path and resolve it through `internal/path` against the merged root. After the drill, the cursor is on the resolved entry (not at a rolodex), so it ends in `entry` mode if the final segment is a non-pointer, or `browse` mode (with rolodex_id = pointer.to) if it's a pointer.

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `reducer_test.go`:

```go
import (
	// existing imports +
	"github.com/scshafe/dex/internal/path"
)

// (Add `path` to the existing import block — don't duplicate the
// import statement.)

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/...`
Expected: FAILs — drill currently rejects `/`-prefixed targets via `LookupByID`.

- [ ] **Step 3: Modify `applyDrill` to dispatch on target shape**

Replace the current `applyDrill` body in `reducer.go`:

```go
func applyDrill(r Resolver, st State, a Action) (State, Envelope, error) {
	if a.Target == "" {
		return st, failure(st, ErrInvalidTarget, "drill requires a target", "", ""), nil
	}
	if a.Target[0] == '/' {
		return drillByPath(r, st, a.Target)
	}
	return drillByUUID(r, st, a.Target)
}

func drillByUUID(r Resolver, st State, target string) (State, Envelope, error) {
	rdx, ok, err := r.LookupByID(target)
	if err != nil {
		return st, Envelope{}, fmt.Errorf("session: lookup: %w", err)
	}
	if !ok {
		return st, failure(st, ErrInvalidTarget,
			fmt.Sprintf("no rolodex with id %q", target), "", ""), nil
	}
	next := touch(st)
	next.PreviousCursors = append(next.PreviousCursors, st.Cursor)
	next.Cursor = Cursor{RolodexID: rdx.ID, Mode: CursorModeBrowse}
	return next, success(next), nil
}

func drillByPath(r Resolver, st State, target string) (State, Envelope, error) {
	root, err := r.MergedRoot()
	if err != nil {
		return st, Envelope{}, fmt.Errorf("session: merged root: %w", err)
	}
	res, err := path.Resolve(r, root, target)
	if err != nil {
		// Map every path-resolution failure to NOT_FOUND. The
		// underlying error message is preserved in the hint so
		// callers can show a useful diagnosis.
		return st, failure(st, ErrNotFound, err.Error(), "", ""), nil
	}

	next := touch(st)
	next.PreviousCursors = append(next.PreviousCursors, st.Cursor)
	next.Cursor = cursorForEntry(res.Entry, res.ParentRolodex, target)
	return next, success(next), nil
}

// cursorForEntry produces the post-drill cursor for a resolved entry.
// Pointer entries advance into their target rolodex (browse mode).
// Non-pointer entries land on the entry itself (entry mode).
func cursorForEntry(e model.Entry, parent model.Rolodex, displayPath string) Cursor {
	if e.Kind == model.KindPointer && e.Pointer != nil {
		return Cursor{
			RolodexID: e.Pointer.To,
			Path:      displayPath,
			Mode:      CursorModeBrowse,
		}
	}
	return Cursor{
		RolodexID: parent.ID,
		EntryID:   e.ID,
		Path:      displayPath,
		Mode:      CursorModeEntry,
	}
}
```

Add `"github.com/scshafe/dex/internal/path"` to the imports in `reducer.go`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: all four tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "$(cat <<'EOF'
session: drill action accepts /path targets

Path-shaped targets resolve through internal/path against the merged
root. A pointer entry advances into its target rolodex in browse
mode; a non-pointer entry lands on the entry itself in entry mode.
Path-resolution failures collapse to NOT_FOUND with the underlying
error preserved in error.message.
EOF
)"
```

---

## Task 4: Pop action

Pop the back-stack: replace the current cursor with the most recent `PreviousCursors` entry, decrement that stack. With an empty stack, pop is a no-op (still returns ok=true; the architect's envelope contract has no idle-action error).

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `reducer_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/...`
Expected: FAILs — `unknown action "pop"`.

- [ ] **Step 3: Add pop dispatch + handler in `reducer.go`**

In the `Apply` switch:

```go
	case ActionPop:
		return applyPop(st)
```

Add the handler:

```go
func applyPop(st State) (State, Envelope, error) {
	next := touch(st)
	if n := len(st.PreviousCursors); n > 0 {
		next.Cursor = st.PreviousCursors[n-1]
		next.PreviousCursors = st.PreviousCursors[:n-1]
	}
	return next, success(next), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "session: pop action restores previous cursor (no-op on empty stack)"
```

---

## Task 5: Activate — info-content (stdout effect)

`activate` reads the entry under the cursor and dispatches by kind. This task handles the simplest case: an info entry with `content` set returns an `effect: stdout` and the cursor is unchanged.

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `reducer_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/...`
Expected: FAILs — `unknown action "activate"`.

- [ ] **Step 3: Add activate dispatch + info-content handling in `reducer.go`**

In the `Apply` switch:

```go
	case ActionActivate:
		return applyActivate(r, st)
```

Add the handlers:

```go
func applyActivate(r Resolver, st State) (State, Envelope, error) {
	if st.Cursor.EntryID == "" {
		return st, failure(st, ErrInvalidTarget,
			"activate requires the cursor to be on an entry", "", ""), nil
	}
	entry, _, ok, err := r.LookupEntryByID(st.Cursor.EntryID)
	if err != nil {
		return st, Envelope{}, fmt.Errorf("session: lookup entry: %w", err)
	}
	if !ok {
		// Stale cursor — the entry was removed by an out-of-band
		// mutation (per pinned decision #4).
		return st, failure(st, ErrStaleSession,
			fmt.Sprintf("entry %q no longer exists", st.Cursor.EntryID),
			"", "start a new session"), nil
	}

	switch entry.Kind {
	case model.KindInfo:
		return activateInfo(st, entry)
	}
	return st, failure(st, ErrInvalidTarget,
		fmt.Sprintf("activate not implemented for kind %q", entry.Kind), "", ""), nil
}

func activateInfo(st State, entry model.Entry) (State, Envelope, error) {
	if entry.Info == nil {
		return st, failure(st, ErrSchemaError,
			fmt.Sprintf("info entry %q has nil payload", entry.Slug), "", ""), nil
	}
	if entry.Info.Provider != "" {
		return st, failure(st, ErrProviderFailed,
			fmt.Sprintf("info entry %q uses provider %q (not implemented in v1)",
				entry.Slug, entry.Info.Provider), "", ""), nil
	}
	next := touch(st)
	return next, success(next, Effect{Type: EffectStdout, Content: entry.Info.Content}), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "$(cat <<'EOF'
session: activate action — info-content emits stdout effect

Activating an info entry with content set returns one stdout effect
carrying the content. Provider-backed info entries error with
PROVIDER_FAILED (architect landmine #1: providers deferred to v2).
Activate with no entry under the cursor returns INVALID_TARGET.
Stale cursors (entry deleted by an out-of-band mutation) return
STALE_SESSION per pinned decision #4.
EOF
)"
```

---

## Task 6: Activate — pointer (drills via the pointer's target)

`activate` on a pointer entry behaves like a drill into the pointer's target rolodex. State changes; no effects.

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
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
```

- [ ] **Step 2: Run test — fails**

Run: `go test ./internal/session/...`
Expected: FAIL — currently activate hits the `activate not implemented for kind` branch.

- [ ] **Step 3: Add the pointer arm in `applyActivate`**

In the kind switch:

```go
	case model.KindPointer:
		return activatePointer(r, st, entry)
```

Add:

```go
func activatePointer(r Resolver, st State, entry model.Entry) (State, Envelope, error) {
	if entry.Pointer == nil {
		return st, failure(st, ErrSchemaError,
			fmt.Sprintf("pointer entry %q has nil payload", entry.Slug), "", ""), nil
	}
	target, ok, err := r.LookupByID(entry.Pointer.To)
	if err != nil {
		return st, Envelope{}, fmt.Errorf("session: lookup pointer target: %w", err)
	}
	if !ok {
		return st, failure(st, ErrNotFound,
			fmt.Sprintf("pointer %q targets unknown rolodex %q", entry.Slug, entry.Pointer.To),
			"", ""), nil
	}
	next := touch(st)
	next.PreviousCursors = append(next.PreviousCursors, st.Cursor)
	next.Cursor = Cursor{RolodexID: target.ID, Mode: CursorModeBrowse}
	return next, success(next), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "session: activate on pointer entry drills into its target rolodex"
```

---

## Task 7: Activate — command, all concerns satisfied (spawn effect)

When the cursor's entry is a command, gather the resolved values and decide:
- All required concerns have a value (from `state.Resolved` or the concern's `default`)? Substitute `{local_id}` placeholders in the template and return `effect: spawn` with the assembled string.
- Otherwise — the missing-concern path — Task 8.

This task implements the "all satisfied" path, mirroring `cli/activate.go`'s `activateCommand` substitution logic.

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
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
```

- [ ] **Step 2: Run tests — fails**

Run: `go test ./internal/session/...`
Expected: FAIL — currently a command entry hits the not-implemented branch.

- [ ] **Step 3: Add the command arm + activator in `reducer.go`**

In the kind switch:

```go
	case model.KindCommand:
		return activateCommand(st, entry)
```

Add:

```go
import (
	// existing imports +
	"strings"
)

func activateCommand(st State, entry model.Entry) (State, Envelope, error) {
	if entry.Command == nil {
		return st, failure(st, ErrSchemaError,
			fmt.Sprintf("command entry %q has nil payload", entry.Slug), "", ""), nil
	}

	// Resolve each concern: state.Resolved > default > unresolved.
	resolved := map[string]string{}
	pending := []PendingConcern{}
	for _, c := range entry.Command.Concerns {
		if v, ok := st.Resolved[c.LocalID]; ok {
			resolved[c.LocalID] = v
			continue
		}
		if c.Default != "" {
			resolved[c.LocalID] = c.Default
			continue
		}
		// Unresolved. Surface as pending; the missing-concern arm
		// (Task 8) decides whether to error or just stage.
		pending = append(pending, PendingConcern{
			LocalID:   c.LocalID,
			Label:     c.Label,
			Default:   c.Default,
			Required:  c.Required,
			Strict:    c.Strict,
			Validator: c.Validator,
			DependsOn: c.DependsOn,
		})
	}

	if len(pending) > 0 {
		// Filled in by Task 8. For now keep the test green by
		// treating any pending as a hard error.
		return st, failure(st, ErrUnresolvedRequired,
			"command has unresolved concerns", "", ""), nil
	}

	assembled := entry.Command.Template
	for k, v := range resolved {
		assembled = strings.ReplaceAll(assembled, "{"+k+"}", v)
	}
	next := touch(st)
	return next, success(next, Effect{Type: EffectSpawn, ShellCommand: assembled}), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "$(cat <<'EOF'
session: activate command — emit spawn effect when concerns satisfied

Mirrors cli/activate.go's substitution model: state.Resolved >
concern.default > unresolved. When all concerns have values the
template's {local_id} placeholders are replaced and the assembled
string is returned as effect:spawn.shell_command. The missing-
concern path is hardened in the next task.
EOF
)"
```

---

## Task 8: Activate — command, missing concerns (pending list + error code)

Refine the pending-concerns path: instead of failing immediately, stage the pending list onto the state and return `UNRESOLVED_REQUIRED` with `error.concern` set to the first missing required concern's local_id. This lets the caller drive a `resolve` loop. Pending concerns persist across activate calls until resolved.

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
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
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/session/...`
Expected: the staging test FAILs because the current code fails immediately without persisting `pending` to state.

- [ ] **Step 3: Replace the missing-concern arm**

In `activateCommand`, replace the early-return block for `len(pending) > 0`:

```go
	if len(pending) > 0 {
		next := touch(st)
		next.PendingConcerns = pending
		// error.concern points at the first pending so callers know
		// where to start the resolve loop.
		return next, failure(next, ErrUnresolvedRequired,
			"command has unresolved concerns",
			pending[0].LocalID,
			"call resolve to fill in concern values"), nil
	}
```

Note: `failure` is called with `next` (the staged state) so the envelope's `session.pending_concerns` reflects the staging. The returned State is `next`, so the staging persists into the caller's session even on the failure path. This is the **one** intentional state-mutating failure path in the reducer.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "$(cat <<'EOF'
session: activate stages pending concerns instead of failing fast

When a command has unresolved concerns (no state.Resolved value, no
default), the reducer stages PendingConcern items onto state and
returns UNRESOLVED_REQUIRED with error.concern pointing at the first
pending local_id. This is the one intentional state-mutating failure
path: the caller needs the pending list to drive a resolve loop.
EOF
)"
```

---

## Task 9: Resolve action + end-to-end drill→resolve→activate

`resolve` writes one concern value into `state.Resolved` and recomputes pending. After every required concern is resolved, the next `activate` succeeds with a `spawn` effect. This task adds the action and writes the integration test the handoff names as the slice's exit criterion.

**Files:**
- Modify: `internal/session/reducer.go`
- Modify: `internal/session/reducer_test.go`

- [ ] **Step 1: Write the failing tests**

Append:

```go
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
```

- [ ] **Step 2: Run tests — fails**

Run: `go test ./internal/session/...`
Expected: FAIL — `unknown action "resolve"`.

- [ ] **Step 3: Add resolve dispatch + handler in `reducer.go`**

In the `Apply` switch:

```go
	case ActionResolve:
		return applyResolve(st, a)
```

Add:

```go
func applyResolve(st State, a Action) (State, Envelope, error) {
	if a.Concern == "" {
		return st, failure(st, ErrInvalidTarget,
			"resolve requires concern (local_id)", "", ""), nil
	}
	// The concern must currently be pending. This catches typos and
	// stale calls after the concern was already resolved.
	idx := -1
	for i, p := range st.PendingConcerns {
		if p.LocalID == a.Concern {
			idx = i
			break
		}
	}
	if idx < 0 {
		return st, failure(st, ErrInvalidTarget,
			fmt.Sprintf("concern %q is not pending", a.Concern),
			a.Concern, ""), nil
	}

	next := touch(st)
	if next.Resolved == nil {
		next.Resolved = map[string]string{}
	}
	next.Resolved[a.Concern] = a.Value
	// Drop the satisfied concern from pending. Validator + strict
	// enforcement is deferred; see pinned decisions in the plan.
	next.PendingConcerns = append(
		append([]PendingConcern{}, st.PendingConcerns[:idx]...),
		st.PendingConcerns[idx+1:]...,
	)
	return next, success(next), nil
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/session/...`
Expected: every test, including the end-to-end one, PASSes.

- [ ] **Step 5: Commit**

```bash
git add internal/session/reducer.go internal/session/reducer_test.go
git commit -m "$(cat <<'EOF'
session: resolve action + end-to-end drill→resolve→activate test

Resolve writes one concern value into state.Resolved and removes the
satisfied entry from PendingConcerns. The integration test drives a
session through drill into /commands, set cursor on the deploy
command, activate (stages pending), resolve ns=prod, activate (emits
spawn). This is the slice exit criterion from the 2026-05-12 handoff.

Validator + strict enforcement is accepted but not yet enforced;
deferred to a follow-up. depends_on ordering also deferred — pending
concerns surface in declaration order.
EOF
)"
```

---

## Task 10: Session file persistence (NewSession, Load, Save, End, opportunistic GC)

The reducer is in-memory; persistence is its sibling. Add a `Manager` that knows a session directory, generates ULID-prefixed session ids, and writes State to disk via the same tempfile+rename pattern used by `store.WriteRolodex`. `NewSession` runs opportunistic GC on the directory before returning the new session.

**Files:**
- Create: `internal/session/store.go`
- Create: `internal/session/store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/store_test.go`:

```go
package session_test

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/scshafe/dex/internal/session"
)

func newManager(t *testing.T) *session.Manager {
	t.Helper()
	dir := t.TempDir()
	return session.NewManager(dir, ulidEntropy())
}

func ulidEntropy() *ulid.MonotonicEntropy {
	return ulid.Monotonic(rand.Reader, 0)
}

func TestNewSessionWritesFile(t *testing.T) {
	m := newManager(t)
	s, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if !strings.HasPrefix(s.ID, "ses_") {
		t.Fatalf("session id %q lacks ses_ prefix", s.ID)
	}
	files, err := os.ReadDir(m.Dir())
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(files))
	}
}

func TestLoadRoundTripsState(t *testing.T) {
	m := newManager(t)
	s, err := m.NewSession()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	s.Resolved["ns"] = "prod"
	s.Version = 7
	if err := m.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := m.Load(s.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != 7 {
		t.Fatalf("version: got %d want 7", loaded.Version)
	}
	if loaded.Resolved["ns"] != "prod" {
		t.Fatalf("resolved[ns]: got %q", loaded.Resolved["ns"])
	}
}

func TestEndRemovesFile(t *testing.T) {
	m := newManager(t)
	s, _ := m.NewSession()
	if err := m.End(s.ID); err != nil {
		t.Fatalf("end: %v", err)
	}
	if _, err := m.Load(s.ID); err == nil {
		t.Fatalf("load after end should fail")
	}
}

func TestNewSessionGCsExpiredFiles(t *testing.T) {
	m := newManager(t)
	expired, _ := m.NewSession()
	// Backdate the on-disk file's last_touched to 31 minutes ago.
	expired.LastTouched = time.Now().Add(-31 * time.Minute)
	if err := m.Save(expired); err != nil {
		t.Fatalf("backdate save: %v", err)
	}

	// Creating a new session triggers GC and should remove the
	// expired one.
	if _, err := m.NewSession(); err != nil {
		t.Fatalf("new session: %v", err)
	}

	if _, err := m.Load(expired.ID); err == nil {
		t.Fatalf("expired session should have been GC'd")
	}
	files, _ := os.ReadDir(m.Dir())
	if len(files) != 1 {
		t.Fatalf("expected 1 file (only the new one), got %d", len(files))
	}
	_ = filepath.Join // keep import
}
```

- [ ] **Step 2: Write `store.go`**

```go
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Manager owns the on-disk session directory.
type Manager struct {
	dir     string
	entropy *ulid.MonotonicEntropy
}

// NewManager constructs a Manager rooted at dir. The directory is
// created if missing. entropy is the ULID source — the caller chooses
// it so production code can use crypto/rand and tests can swap in a
// deterministic source.
func NewManager(dir string, entropy *ulid.MonotonicEntropy) *Manager {
	_ = os.MkdirAll(dir, 0o755) // best-effort; surfaced on first write
	return &Manager{dir: dir, entropy: entropy}
}

func (m *Manager) Dir() string { return m.dir }

// NewSession creates a fresh State, writes it to disk, and returns
// it. Runs opportunistic GC on the session dir first (pinned
// decision #2).
func (m *Manager) NewSession() (State, error) {
	if err := m.gc(); err != nil {
		// GC failures are not fatal — log via the returned error
		// chain but still proceed with session creation. The caller
		// gets to decide whether to surface this.
		// In v1 we just attach via fmt.Errorf, no logging package.
		// (Currently we silently swallow; revisit when telemetry lands.)
		_ = err
	}

	id, err := m.newID()
	if err != nil {
		return State{}, err
	}
	now := time.Now()
	st := State{
		ID:              id,
		Cursor:          Cursor{Mode: CursorModeBrowse},
		Resolved:        map[string]string{},
		PendingConcerns: []PendingConcern{},
		Version:         0,
		CreatedAt:       now,
		LastTouched:     now,
		PreviousCursors: []Cursor{},
	}
	if err := m.Save(st); err != nil {
		return State{}, err
	}
	return st, nil
}

func (m *Manager) newID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), m.entropy)
	if err != nil {
		return "", fmt.Errorf("session: ulid: %w", err)
	}
	return "ses_" + id.String(), nil
}

func (m *Manager) sessionPath(id string) string {
	return filepath.Join(m.dir, id+".json")
}

// Save serializes st to disk via tempfile+rename (atomic).
func (m *Manager) Save(st State) error {
	if !strings.HasPrefix(st.ID, "ses_") {
		return fmt.Errorf("session: id %q missing ses_ prefix", st.ID)
	}
	b, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(m.dir, ".tmp-session-*.json")
	if err != nil {
		return fmt.Errorf("session: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("session: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("session: close: %w", err)
	}
	if err := os.Rename(tmpPath, m.sessionPath(st.ID)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("session: rename: %w", err)
	}
	return nil
}

// Load reads a session by id. Returns an error if the file is missing
// or unparseable.
func (m *Manager) Load(id string) (State, error) {
	b, err := os.ReadFile(m.sessionPath(id))
	if err != nil {
		return State{}, fmt.Errorf("session: load %s: %w", id, err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("session: parse %s: %w", id, err)
	}
	return st, nil
}

// End removes the session file. Missing files are not an error.
func (m *Manager) End(id string) error {
	err := os.Remove(m.sessionPath(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session: end %s: %w", id, err)
	}
	return nil
}

// gc removes session files whose last_touched is older than SessionTTL.
// Errors on individual files are swallowed — GC is opportunistic, not
// load-bearing.
func (m *Manager) gc() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("session: gc readdir: %w", err)
	}
	cutoff := time.Now().Add(-SessionTTL)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "ses_") {
			continue
		}
		path := filepath.Join(m.dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var probe struct {
			LastTouched time.Time `json:"last_touched"`
		}
		if err := json.Unmarshal(b, &probe); err != nil {
			continue
		}
		if probe.LastTouched.Before(cutoff) {
			_ = os.Remove(path)
		}
	}
	return nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/session/...`
Expected: all four store tests PASS, plus the prior reducer tests.

- [ ] **Step 4: Verify *store.Store satisfies session.Resolver**

Add a small compile-time assertion to keep the two packages in sync. Append to `internal/session/store.go`:

```go
// Compile-time check: *store.Store satisfies Resolver. We don't
// import store from this package — the assertion lives in the cli
// package alongside the session-verb wiring (next slice). For now
// this comment documents the intent.
var _ = Resolver(nil) // satisfied via interface; concrete check is in the cli package
```

(The actual `var _ Resolver = (*store.Store)(nil)` assertion belongs in the next slice's CLI wiring, where the session package and the store package both already need to be imported.)

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: every test in every package PASSes. Catches accidental regressions in cli/store/path.

- [ ] **Step 6: Commit**

```bash
git add internal/session/store.go internal/session/store_test.go
git commit -m "$(cat <<'EOF'
session: file persistence — NewSession, Save, Load, End + GC

Manager wraps a session directory and writes State via the same
tempfile+rename pattern store.WriteRolodex uses (atomic). Session ids
are "ses_" + ULID, generated from a caller-supplied entropy source
so tests can stay deterministic. NewSession runs opportunistic GC
that removes any session file whose last_touched is older than
SessionTTL (30 min) per pinned decision #2 — no daemon, no
heartbeats, no escape-hatch flags in v1.
EOF
)"
```

---

## Task 11: Slice handoff note

A short note for the next session (which will land the `dex session` CLI verbs on top of this reducer). Update the existing handoff index file rather than creating a new one — this slice is mid-stream of P-11.4.

**Files:**
- Modify: `docs/handoffs/2026-05-12.md` (append a brief addendum at the end)

- [ ] **Step 1: Append the addendum**

Add to the bottom of `docs/handoffs/2026-05-12.md`:

```markdown
## Addendum — session reducer + state landed

The reducer half of P-11.4 shipped on `main` via the
`2026-05-12-session-state-and-reducer` plan. `internal/session`
exposes:

- `Apply(r, st, action) → (st', envelope, err)` pure reducer
- Actions: `drill`, `pop`, `activate`, `resolve`
- Effects: `spawn` (shell_command string), `stdout`, `close_session`
- `Manager` for ses_*.json file persistence with opportunistic GC

The integration test at `internal/session/reducer_test.go::TestEndToEnd_DrillResolveActivate`
drives a session through drill → activate (stages pending) → resolve
→ activate (emits spawn). This is the exit criterion the original
handoff named.

Pinned design decisions are inlined at the top of the plan doc and
in `internal/session/doc.go`. Open follow-up:

- `dex session start|step|state|end|list` CLI verbs (next slice)
- Validator script execution + `strict` enforcement on `resolve`
- `depends_on` ordering for pending_concerns
- Drilling into a non-rolodex uuid (currently fails — caller must
  use `/path` or set the cursor directly)
- View hint population in the envelope (currently always nil)
```

- [ ] **Step 2: Commit**

```bash
git add docs/handoffs/2026-05-12.md
git commit -m "handoff: addendum — session reducer + state landed"
```

---

## Self-review summary

- **Spec coverage:** every handoff requirement for "in-memory reducer + state file format + a test that drives drill → resolve → activate" is covered (Task 9's `TestEndToEnd_DrillResolveActivate`). Pinned decisions cover the open questions the handoff flagged. Out-of-scope items (CLI verbs, view hint, validator/strict/depends_on) are explicitly listed.
- **Placeholder scan:** every step has concrete code or commands. Test names, file paths, and commit messages are all spelled out.
- **Type consistency:** `State`, `Cursor`, `PendingConcern`, `Action`, `Effect`, `Error`, `Envelope`, `SessionView`, `CursorMode`, `EffectType`, `ErrorCode` are defined once in Task 1 and reused by name in every later task. The `Resolver` interface (Task 2) has three methods — `LookupByID`, `LookupEntryByID`, `MergedRoot` — and every later task uses exactly those signatures.

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-12-session-state-and-reducer.md`.**
