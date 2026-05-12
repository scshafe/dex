# Path Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement path-style addressing (`dex ls /commands/broker`) as pure sugar over uuid resolution — the engine sees only uuids; paths are canonicalized at the CLI boundary.

**Architecture:** A new `internal/path` package owns the resolution algorithm. It takes a `Resolver` interface (a thing that can `LookupByID`) plus a merged-root rolodex, and walks the slug chain — following `pointer` entries mid-path and returning the final entry regardless of kind (architect's Q3). The store implements the `Resolver` interface. `dex ls` learns to distinguish between path-shaped args (start with `/`), ULID-shaped args, and the no-arg merged-root case.

**Tech Stack:** Go 1.22+ stdlib only. No new dependencies. Algorithm is pure; testable against a fake `Resolver` without disk IO.

---

## Out of scope (for follow-up plans)

- `@<visibility>` anchor syntax (e.g. `/commands/broker@bundled`) — architect's Q3 mentioned this but it's not load-bearing for v1
- `~` session-cursor shorthand
- The other stateless verbs (`explore`, `search`, `activate`)
- The session API and mutation verbs

---

## File Structure

```
dex/
├── internal/
│   ├── path/
│   │   ├── path.go         (Tasks 1–2; package, Resolver iface, Resolve func, errors)
│   │   └── path_test.go    (Tasks 1–2; algorithm-level tests with a fake Resolver)
│   └── cli/
│       ├── ls.go           (Task 3 modify; path-vs-uuid dispatch)
│       └── ls_test.go      (Task 3 modify; end-to-end path tests via temp-dir fixtures)
└── docs/superpowers/plans/2026-05-12-path-resolution.md
```

The `Resolver` interface is intentionally narrow (one method) so the package is reusable later by `dex explore`, `dex search`, and the session API without coupling to the store package's full surface.

---

## Task 1: Path Package — Single-Segment Resolution

Set up the package with the `Resolver` interface and a `Resolve` function that handles only paths with one segment (no pointer following yet). This gets the wiring done.

**Files:**
- Create: `internal/path/path.go`
- Create: `internal/path/path_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/path/path_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/path/...`
Expected: FAIL (package does not exist).

- [ ] **Step 3: Write minimal implementation**

Create `internal/path/path.go`:
```go
// Package path resolves dex paths (e.g. "/commands/broker") to entries.
//
// Architect's Q3: paths are pure sugar over uuid; the engine sees only
// uuids; paths are canonicalized at the CLI boundary. Resolution walks
// the slug chain starting at the merged root, following `pointer`
// entries mid-path; the final segment can be any kind.
package path

import (
	"errors"
	"fmt"
	"strings"

	"github.com/scshafe/dex/internal/model"
)

// Resolver looks up a Rolodex by its ULID. The path package depends on
// this narrow interface rather than the store package directly, so it
// stays algorithm-only and is reusable.
type Resolver interface {
	LookupByID(id string) (model.Rolodex, bool, error)
}

// Result is the outcome of a successful Resolve.
type Result struct {
	// Entry is the final-segment entry, regardless of kind.
	Entry model.Entry
	// ParentRolodex is the rolodex that contains Entry. For first-segment
	// paths this is the merged root passed into Resolve.
	ParentRolodex model.Rolodex
}

var (
	// ErrNotFound is returned when a path segment doesn't match any slug.
	ErrNotFound = errors.New("path: not found")
	// ErrTraversesNonPointer is returned when a mid-path segment exists
	// but is not a pointer entry (so resolution cannot continue).
	ErrTraversesNonPointer = errors.New("path: traverses non-pointer entry")
	// ErrCycle is returned when path resolution exceeds the depth cap,
	// which catches both pointer cycles and pathologically deep chains.
	ErrCycle = errors.New("path: cycle or unreasonably deep chain")
	// ErrSyntax is returned when the path doesn't start with "/" or is empty.
	ErrSyntax = errors.New("path: invalid syntax")
)

// Resolve walks the path from mergedRoot, returning the final-segment
// entry. Paths must start with "/". Trailing slashes are ignored.
//
// Task 1 supports only single-segment paths. Task 2 extends to
// multi-segment with pointer traversal.
func Resolve(r Resolver, mergedRoot model.Rolodex, p string) (Result, error) {
	if !strings.HasPrefix(p, "/") {
		return Result{}, fmt.Errorf("%w: must start with %q (got %q)", ErrSyntax, "/", p)
	}
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return Result{}, fmt.Errorf("%w: empty path", ErrSyntax)
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) > 1 {
		return Result{}, fmt.Errorf("%w: multi-segment paths not yet supported", ErrSyntax)
	}
	seg := segments[0]
	for _, e := range mergedRoot.Entries {
		if e.Slug == seg {
			return Result{Entry: e, ParentRolodex: mergedRoot}, nil
		}
	}
	return Result{}, fmt.Errorf("%w: %q in %s", ErrNotFound, seg, mergedRoot.Slug)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/path/...`
Expected: PASS — six tests:
- TestResolveSingleSegmentPointer
- TestResolveSingleSegmentNonPointer
- TestResolveNotFound
- TestResolveEmptyPath
- TestResolveRequiresLeadingSlash
- TestResolveTrailingSlashIgnored

- [ ] **Step 5: Commit**

```bash
git add internal/path/path.go internal/path/path_test.go
git commit -m "$(cat <<'EOF'
Add path package with single-segment Resolve

New internal/path package owns the path → entry resolution algorithm.
Q3-aligned: paths must start with '/', trailing slashes are ignored,
final segment can be any kind. This task lays the package scaffold,
errors, and Resolver interface; Task 2 adds multi-segment traversal.
EOF
)"
```

---

## Task 2: Multi-Segment Resolution with Pointer Traversal + Depth Cap

Extend `Resolve` to walk multi-segment paths, following `pointer` entries to traverse rolodex boundaries. Add a depth cap that catches both true cycles and pathological depth.

**Files:**
- Modify: `internal/path/path.go`
- Modify: `internal/path/path_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/path/path_test.go`:
```go
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
```

Note: the cycle test needs `strings` imported in the test file. The Task 1 tests don't currently import it. Add `"strings"` to the test file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/path/...`
Expected: FAIL — the new tests need multi-segment support.

- [ ] **Step 3: Replace `Resolve` with the full implementation**

Replace the `Resolve` function in `internal/path/path.go` with:
```go
// MaxDepth is the hard cap on resolution hops. Each segment counts as
// one hop. A path that exceeds the cap returns ErrCycle — which catches
// both genuine pointer cycles and pathologically deep chains.
const MaxDepth = 32

// Resolve walks the path from mergedRoot, following pointer entries
// mid-path and returning the final-segment entry (any kind). Paths must
// start with "/". Trailing slashes are ignored.
func Resolve(r Resolver, mergedRoot model.Rolodex, p string) (Result, error) {
	if !strings.HasPrefix(p, "/") {
		return Result{}, fmt.Errorf("%w: must start with %q (got %q)", ErrSyntax, "/", p)
	}
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return Result{}, fmt.Errorf("%w: empty path", ErrSyntax)
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) > MaxDepth {
		return Result{}, fmt.Errorf("%w: %d segments exceeds cap of %d", ErrCycle, len(segments), MaxDepth)
	}

	currentRolodex := mergedRoot
	for i, seg := range segments {
		var entry model.Entry
		found := false
		for _, e := range currentRolodex.Entries {
			if e.Slug == seg {
				entry = e
				found = true
				break
			}
		}
		if !found {
			return Result{}, fmt.Errorf("%w: %q in %s", ErrNotFound, seg, currentRolodex.Slug)
		}

		// Final segment: return regardless of kind.
		if i == len(segments)-1 {
			return Result{Entry: entry, ParentRolodex: currentRolodex}, nil
		}

		// Mid-path: must be a pointer to continue traversal.
		if entry.Kind != model.KindPointer {
			return Result{}, fmt.Errorf("%w: %q is %s", ErrTraversesNonPointer, seg, entry.Kind)
		}
		if entry.Pointer == nil {
			return Result{}, fmt.Errorf("%w: pointer entry %q has nil payload", ErrTraversesNonPointer, seg)
		}

		next, ok, err := r.LookupByID(entry.Pointer.To)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, fmt.Errorf("%w: pointer target %q", ErrNotFound, entry.Pointer.To)
		}
		currentRolodex = next
	}

	// Unreachable: the loop always returns or errors.
	return Result{}, fmt.Errorf("%w: unreachable", ErrSyntax)
}
```

- [ ] **Step 4: Run test to verify everything passes**

Run: `go test -v ./internal/path/...`
Expected: all ten tests pass (six from Task 1 + four new: TwoSegment, TraversesNonPointer, DanglingPointer, Cycle).

- [ ] **Step 5: Commit**

```bash
git add internal/path/path.go internal/path/path_test.go
git commit -m "$(cat <<'EOF'
Add multi-segment path resolution with pointer traversal

Resolve now walks multi-segment paths, following pointer entries to
cross rolodex boundaries. Depth cap of 32 hops catches both cycles and
pathological chains (architect Q3). Dangling pointers surface as
ErrNotFound; non-pointer mid-path entries surface as ErrTraversesNonPointer.
EOF
)"
```

---

## Task 3: Wire `dex ls /path` Into the CLI

Teach `dex ls` to distinguish path arguments (start with `/`) from ULID arguments. Drill through pointer entries; refuse to `ls` non-pointer leaves with a clear hint.

**Files:**
- Modify: `internal/cli/ls.go`
- Modify: `internal/cli/ls_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/ls_test.go`:
```go
func TestLsByPathRoot(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp) // bundled root with /tools (pointer to T1)
	// Add the target rolodex so the pointer resolves.
	target := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000T1",
		"slug": "tools-collection",
		"label": "Tools collection",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000C1",
				"slug": "hammer",
				"label": "Hammer",
				"kind": "info",
				"info": { "content": "the hammer" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "tools.json"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/tools"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "hammer" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsByPathSlashListsRoot(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "tools" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsByPathNonPointerErrors(t *testing.T) {
	tmp := t.TempDir()
	// Bundled root with /readme (info, not pointer).
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	rolodex := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Bundled root",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000E1",
				"slug": "readme",
				"label": "Readme",
				"kind": "info",
				"info": { "content": "hi" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/readme"})
	if exit == 0 {
		t.Fatalf("expected non-zero exit for ls on info entry")
	}
	if !strings.Contains(errBuf.String(), "explore") {
		t.Fatalf("expected stderr to suggest 'explore', got %q", errBuf.String())
	}
}

func TestLsByPathNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/does-not-exist"})
	if exit == 0 {
		t.Fatalf("expected non-zero exit for unknown path")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr, got %q", errBuf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — the path argument isn't recognized; existing uuid arm rejects `/tools` etc.

- [ ] **Step 3: Modify `internal/cli/ls.go`**

Add `"strings"` and `"github.com/scshafe/dex/internal/path"` to the import block (the file currently imports `encoding/json`, `fmt`, `io`, `os`, `internal/model`, `internal/store`).

Replace the entire `case 1:` arm inside `RunLs` (currently calls `s.LookupByID`) with:
```go
case 1:
	arg := argv[0]
	if strings.HasPrefix(arg, "/") {
		entries, err = resolvePath(s, arg, opts.Stderr)
		if err != nil {
			return 1
		}
	} else {
		r, ok, err := s.LookupByID(arg)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(opts.Stderr, "dex ls: rolodex %q not found\n", arg)
			return 1
		}
		entries = r.Entries
	}
```

Add the `resolvePath` helper as a new top-level function in the same file (after `RunLs` and before `emitJSON`):
```go
// resolvePath handles the path arm of `dex ls`. Special-cases "/" as
// "list merged root"; otherwise walks the path via internal/path and
// drills if the final entry is a pointer.
func resolvePath(s *store.Store, p string, stderr io.Writer) ([]model.Entry, error) {
	root, err := s.MergedRoot()
	if err != nil {
		fmt.Fprintf(stderr, "dex ls: %v\n", err)
		return nil, err
	}
	if p == "/" {
		return root.Entries, nil
	}

	result, err := path.Resolve(s, root, p)
	if err != nil {
		fmt.Fprintf(stderr, "dex ls: %v\n", err)
		return nil, err
	}
	if result.Entry.Kind != model.KindPointer {
		fmt.Fprintf(stderr,
			"dex ls: %q is a %s entry; use `dex explore` or `dex activate` instead\n",
			p, result.Entry.Kind)
		return nil, fmt.Errorf("not a pointer")
	}
	if result.Entry.Pointer == nil {
		fmt.Fprintf(stderr, "dex ls: pointer entry %q has nil payload\n", p)
		return nil, fmt.Errorf("nil pointer payload")
	}
	target, ok, err := s.LookupByID(result.Entry.Pointer.To)
	if err != nil {
		fmt.Fprintf(stderr, "dex ls: %v\n", err)
		return nil, err
	}
	if !ok {
		fmt.Fprintf(stderr, "dex ls: dangling pointer at %q (target %q)\n",
			p, result.Entry.Pointer.To)
		return nil, fmt.Errorf("dangling pointer")
	}
	return target.Entries, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: all tests pass — the 4 new cli tests plus everything from prior tasks.

- [ ] **Step 5: Smoke-test the binary**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-path-smoke
mkdir -p /tmp/dex-path-smoke/{bundled,personal,private,ephemeral}
cat > /tmp/dex-path-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Bundled root",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HB00000000000000000000E1",
      "slug": "tools",
      "label": "Tools",
      "kind": "pointer",
      "pointer": { "to": "01HB00000000000000000000T1" }
    }
  ]
}
JSON
cat > /tmp/dex-path-smoke/bundled/tools.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000T1",
  "slug": "tools-collection",
  "label": "Tools collection",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HB00000000000000000000C1",
      "slug": "hammer",
      "label": "Hammer",
      "kind": "info",
      "info": { "content": "the hammer" }
    }
  ]
}
JSON

DEX_STORE=/tmp/dex-path-smoke ./dex ls /              # should show "tools"
DEX_STORE=/tmp/dex-path-smoke ./dex ls /tools         # should show "hammer"
DEX_STORE=/tmp/dex-path-smoke ./dex ls /tools/hammer  # should error: 'use explore'
DEX_STORE=/tmp/dex-path-smoke ./dex ls /missing       # should error: 'not found'
DEX_STORE=/tmp/dex-path-smoke ./dex ls --json /tools  # JSON form of /tools

rm -rf /tmp/dex-path-smoke
```

Each should behave per the comment. Note that `/tools/hammer` is an `info` entry whose final segment is reachable, so resolution succeeds — but `dex ls` refuses to list it (suggests explore/activate).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/ls.go internal/cli/ls_test.go
git commit -m "$(cat <<'EOF'
Wire dex ls </path> via internal/path

`dex ls` now accepts three arg forms: nothing (merged root), a ULID
(direct lookup), or a path starting with "/" (slug walk through
pointer entries). Non-pointer leaves get a clear "use explore/activate"
hint rather than a confusing empty listing or silent success.
EOF
)"
```

---

## Self-Review

**Spec coverage:**
- Architect Q3 "paths are pure sugar over uuid; canonicalized at the CLI boundary": ✓ — `internal/path` is a pure algorithm over the `Resolver` interface; `RunLs` calls it.
- "Resolution = slug walk through pointer entries": ✓ — Task 2 algorithm.
- "If the entry is the final segment, it's the target regardless of kind": ✓ — Tasks 1 and 2 final-segment branches return any kind; `dex ls` separately enforces "pointer for drilling" semantics.
- "Cycles capped at 32 hops": ✓ — Task 2 `MaxDepth = 32`, `ErrCycle` returned.
- "Pointer/rolodex `to` fields must be ULIDs, never paths": ✓ — already enforced by JSON Schema in P-11.2 slice; nothing to add here.
- "Trailing-slash semantics: identical to no trailing slash": ✓ — Task 1 test + algorithm.

**Out of scope (deliberately):**
- `<slug>@<visibility>` anchors — follow-up plan
- `~` cursor shorthand — follow-up plan
- Other verbs — follow-up plans

**Placeholder scan:** no TBDs, no "TODO", every step has working code or an explicit command.

**Type consistency:**
- `path.Resolver` interface: one method, `LookupByID(string) (model.Rolodex, bool, error)` — exact match to `store.Store.LookupByID`'s signature, so `*store.Store` satisfies it without an adapter.
- `path.Result` struct: `Entry model.Entry`, `ParentRolodex model.Rolodex` — used consistently across Tasks 1, 2, 3.
- `path.MaxDepth`, `path.ErrNotFound`, `path.ErrTraversesNonPointer`, `path.ErrCycle`, `path.ErrSyntax` — all defined in Task 1 (except `MaxDepth` added in Task 2) and referenced by tests.
- `resolvePath` helper in `internal/cli/ls.go`: returns `([]model.Entry, error)`, takes `*store.Store` — consistent with how `RunLs` uses it.

One thing to watch during execution: `internal/cli/ls.go` currently uses `:=` to assign `s` from `store.Open`. The new `case 1:` arm above uses `entries, err = ...` which means `err` must already be in scope. Look at the current `case 1:` and the surrounding switch — the implementer should adapt the variable declarations as needed to keep the file compiling. The intent is preserved either way.
