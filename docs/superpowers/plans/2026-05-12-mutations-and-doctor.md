# Mutation CLI + `dex doctor` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the write surface from the P-11.8 design — `dex add / edit / rm / promote` — plus the `dex doctor` validator that catches schema drift and dangling pointers. Closes the agent-write half-loop (`explore → search → add → activate`) and provides the atomic-write + lockfile infrastructure that the session API (P-11.4) will sit on.

**Architecture:** All writes go through a single `Store.WriteRolodex(r)` choke point that validates against the embedded schema, takes a per-rolodex lockfile, and writes via tempfile + rename. The four mutation verbs are thin wrappers over read-modify-write loops on this primitive. `dex doctor` is read-only — schema-validates every file, scans for dangling pointers (any `pointer.to` / `rolodex.to` / `concern.rolodex.to` whose target doesn't exist), and reports findings.

**Tech Stack:** Go 1.22+ stdlib only (`os`, `syscall.Flock` via `golang.org/x/sys/unix` if needed — but stdlib `os` rename + a `.lock` file with `O_CREATE|O_EXCL` is enough for v1). `github.com/oklog/ulid/v2` for ULID generation. No new deps beyond what's already imported.

---

## Out of scope (deliberately deferred)

- **Creating new rolodexes from scratch.** Slug-collision policy for new top-level rolodexes is a design decision worth its own conversation. v1 mutation surface assumes rolodexes already exist on disk; `dex add` only adds *entries* to existing rolodexes.
- **Editing or adding concerns on command entries.** The flag surface for nested concerns is fiddly; agents and humans editing commands today should use `--from-json` (full entry replacement). A focused `dex concern add/edit/rm` slice can come later.
- **Tombstones for `dex rm`.** The design mentions retaining ULIDs for backlink-safety; v1 just removes the entry from its parent rolodex's `entries` array. If a pointer somewhere targets the removed entry, `dex doctor` will flag it. Backlink-safe tombstones are a follow-up.
- **`dex tree`.** Listed as a stateless verb in the design but independent of writes; defer to a polish slice.
- **`dex add --new-rolodex` flow.** See above.
- **Validator + strict enforcement on concerns.** Still deferred from P-11.2.

---

## File Structure

```
dex/
├── internal/
│   ├── cli/
│   │   ├── add.go              Task 2
│   │   ├── add_test.go         Task 2
│   │   ├── edit.go             Task 3
│   │   ├── edit_test.go        Task 3
│   │   ├── rm.go               Task 4
│   │   ├── rm_test.go          Task 4
│   │   ├── promote.go          Task 5
│   │   ├── promote_test.go     Task 5
│   │   ├── doctor.go           Task 6
│   │   └── doctor_test.go      Task 6
│   └── store/
│       ├── write.go            Task 1 (new file — atomic write + lockfile + WriteRolodex)
│       └── write_test.go       Task 1
└── cmd/dex/
    └── main.go                 Tasks 2-6 (verb dispatch wiring)
```

Putting writes in `internal/store/write.go` (separate from the existing `store.go` which is reads + traversal) keeps the file focused — `store.go` has grown to ~200 lines and adding write primitives would push it past comfortable browse.

---

## Task 1: Write Infrastructure (`Store.WriteRolodex`)

Single entry point for every mutation. Validates → locks → tempfile-writes → renames. Per-rolodex lockfile prevents two concurrent writers from corrupting the same JSON file. Schema validation happens on the *resulting* in-memory `Rolodex` before any write, so bad mutations error before touching disk.

**Files:**
- Create: `internal/store/write.go`
- Create: `internal/store/write_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/store/write_test.go`:
```go
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
	// Verify a single file landed under bundled/ with the expected content.
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
	// Round-trip via LoadTier.
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
	// Slug "BadSlug" violates the kebab-case pattern.
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
	// Still exactly one file (rewrite, not new file).
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
	// Two goroutines writing to the same rolodex id; both must succeed
	// without producing a corrupt file. Final state is one valid file.
	s, root := newWritableStore(t)
	const id = "01HB00000000000000000000R1"
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			r := sampleRolodex(id, "root", model.VisibilityBundled)
			r.Label = label
			_ = s.WriteRolodex(r) // either succeeds or returns lock error; both acceptable
		}("label-" + filepath.Base(t.TempDir()))
	}
	wg.Wait()

	// Read whatever is on disk; it must parse successfully (no half-written file).
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
	// Filename should follow <slug>.<short-id>.json convention so the
	// filesystem is human-browsable even though uuids are canonical.
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL — `WriteRolodex` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/store/write.go`:
```go
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/schema"
)

// WriteRolodex is the single choke point for every mutation. It:
//  1. Schema-validates the in-memory rolodex (rejects bad data before disk).
//  2. Acquires a per-rolodex lockfile (prevents concurrent corruption).
//  3. Writes the JSON to a tempfile and renames it into place (atomic).
//
// The file is placed under <root>/<visibility>/<slug>.<short-id>.json. If a
// file with the same rolodex id already exists under that tier, it is
// overwritten in place; otherwise a new file is created. Files in other
// tiers with the same id are ignored (callers should use `dex promote`
// to move rolodexes between tiers).
func (s *Store) WriteRolodex(r model.Rolodex) error {
	dir, ok := s.tiers[r.Visibility]
	if !ok {
		return fmt.Errorf("store: unknown visibility %q", r.Visibility)
	}

	// 1. Validate against the embedded schema.
	b, err := json.MarshalIndent(&r, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal: %w", err)
	}
	var parsed any
	if err := json.Unmarshal(b, &parsed); err != nil {
		return fmt.Errorf("store: re-parse for validation: %w", err)
	}
	if err := schema.Validate(parsed); err != nil {
		return fmt.Errorf("store: schema: %w", err)
	}

	// 2. Acquire a lockfile keyed on the rolodex id.
	lockPath := filepath.Join(dir, ".lock."+r.ID)
	lock, err := acquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("store: lock: %w", err)
	}
	defer releaseLock(lock, lockPath)

	// 3. Find the target file (if it exists) or pick a new name.
	target, err := findFileForID(dir, r.ID)
	if err != nil {
		return err
	}
	if target == "" {
		target = filepath.Join(dir, fmt.Sprintf("%s.%s.json", r.Slug, shortID(r.ID)))
	}

	// 4. Atomic write: tempfile + rename.
	tmp, err := os.CreateTemp(dir, ".tmp-write-*.json")
	if err != nil {
		return fmt.Errorf("store: create tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("store: write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: rename: %w", err)
	}
	return nil
}

// shortID returns the last 6 characters of the ULID for filename use.
// Filenames are <slug>.<short>.json — the slug is for human browsing,
// the short suffix avoids collisions when two rolodexes share a slug
// across visibilities or after a rename.
func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

// findFileForID scans dir for a .json file containing the given rolodex
// id. Returns the path if found, empty string + nil error if not.
// Errors are reserved for IO problems.
func findFileForID(dir, id string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("readdir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(b, &probe); err != nil {
			continue
		}
		if probe.ID == id {
			return path, nil
		}
	}
	return "", nil
}

// acquireLock opens the lockfile with O_CREATE|O_EXCL. If another writer
// already holds it, returns an error. This is per-rolodex, not per-tier,
// so unrelated mutations don't contend.
//
// v1 is best-effort: we don't block-and-retry. Callers see a fast failure
// on contention. This is fine for an interactive CLI; the session API
// (P-11.4) can layer retry on top if needed.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("acquire %s: %w", path, err)
	}
	return f, nil
}

// releaseLock removes the lockfile and closes the handle. We log but do
// not error on cleanup failures — the next writer will overwrite or
// re-attempt naturally.
func releaseLock(f *os.File, path string) {
	_ = f.Close()
	_ = os.Remove(path)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/...`
Expected: PASS — the 5 new write tests plus all prior store tests.

- [ ] **Step 5: Commit**

```bash
git add internal/store/write.go internal/store/write_test.go
git commit -m "$(cat <<'EOF'
Add Store.WriteRolodex with schema validation + per-rolodex locks

Single choke point for every mutation. Schema-validates the in-memory
rolodex before touching disk, takes a per-rolodex lockfile, writes via
tempfile + rename for atomicity. Files use <slug>.<short-id>.json so the
filesystem is human-browsable even though ulids are canonical. Lock is
best-effort O_EXCL (no retry); callers see a fast failure on contention.
Honors architect landmine #2 (per-rolodex write lock + atomic rename).
EOF
)"
```

---

## Task 2: `dex add`

Add an entry to an existing rolodex. Two modes: flag-based (`--slug X --label Y --kind pointer --pointer-to ULID`) for simple entries, and `--from-json` (stdin or file) for complex entries like commands with concerns. Auto-generates the entry ID if `--id` isn't passed.

**Files:**
- Create: `internal/cli/add.go`
- Create: `internal/cli/add_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/add_test.go`:
```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

// writeAddFixture provisions a store with one rolodex into which entries
// can be added.
func writeAddFixture(t *testing.T, root string) (parentID string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rolodex := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": []
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000R1"
}

func TestAddPointerEntry(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{
			"--parent", parent,
			"--slug", "tools",
			"--label", "Tools",
			"--kind", "pointer",
			"--pointer-to", "01HB00000000000000000000T1",
		})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}
	// Read the rolodex back and confirm the entry landed.
	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			Slug    string `json:"slug"`
			Kind    string `json:"kind"`
			Pointer struct {
				To string `json:"to"`
			} `json:"pointer"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Slug != "tools" || e.Kind != "pointer" || e.Pointer.To != "01HB00000000000000000000T1" {
		t.Fatalf("entry not as expected: %+v", e)
	}
}

func TestAddInfoEntryWithContent(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out},
		[]string{
			"--parent", parent,
			"--slug", "readme",
			"--label", "Readme",
			"--kind", "info",
			"--content", "the body text",
		})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			Slug string `json:"slug"`
			Info struct {
				Content string `json:"content"`
			} `json:"info"`
		} `json:"entries"`
	}
	_ = json.Unmarshal(b, &got)
	if len(got.Entries) != 1 || got.Entries[0].Info.Content != "the body text" {
		t.Fatalf("entry not as expected: %+v", got.Entries)
	}
}

func TestAddFromJSON(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	entryJSON := `{
		"id": "01HB00000000000000000000E1",
		"slug": "broker-status",
		"label": "Broker status",
		"kind": "command",
		"command": {
			"template": "wm broker status --provider {provider}",
			"concerns": [{
				"id": "01HB00000000000000000000K1",
				"local_id": "provider",
				"slug": "provider-concern",
				"label": "Which provider?",
				"required": true,
				"strict": false
			}]
		}
	}`
	entryFile := filepath.Join(tmp, "entry.json")
	if err := os.WriteFile(entryFile, []byte(entryJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"--parent", parent, "--from-json", entryFile})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	if !strings.Contains(string(b), "broker-status") {
		t.Fatalf("entry not added; rolodex content: %s", string(b))
	}
}

func TestAddRejectsUnknownParent(t *testing.T) {
	tmp := t.TempDir()
	writeAddFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{
			"--parent", "01HB00000000000000000000ZZ",
			"--slug", "tools",
			"--label", "Tools",
			"--kind", "pointer",
			"--pointer-to", "01HB00000000000000000000T1",
		})
	if exit == 0 {
		t.Fatal("expected error for unknown parent")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestAddRejectsDuplicateSlug(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	// Add once.
	args := []string{
		"--parent", parent,
		"--slug", "tools",
		"--label", "Tools",
		"--kind", "pointer",
		"--pointer-to", "01HB00000000000000000000T1",
	}
	var out, errBuf bytes.Buffer
	if exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, args); exit != 0 {
		t.Fatalf("first add failed: exit=%d stderr=%q", exit, errBuf.String())
	}
	// Add again with the same slug — must error.
	errBuf.Reset()
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, args)
	if exit == 0 {
		t.Fatal("expected duplicate-slug error")
	}
	if !strings.Contains(errBuf.String(), "duplicate") && !strings.Contains(errBuf.String(), "exists") {
		t.Fatalf("expected duplicate hint in stderr; got %q", errBuf.String())
	}
}

func TestAddAutoGeneratesIDIfMissing(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out},
		[]string{
			"--parent", parent,
			"--slug", "auto",
			"--label", "Auto",
			"--kind", "info",
			"--content", "x",
		})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	// stdout should print the generated id of the new entry.
	if !strings.Contains(out.String(), "01") {
		t.Fatalf("expected generated ULID in stdout; got %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunAdd`, `AddOpts` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/add.go`:
```go
package cli

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type AddOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunAdd implements `dex add --parent <ULID> ...`. Two modes:
//
//   - Flag mode: --slug, --label, --kind, plus kind-specific payload
//     (--pointer-to, or --content). Pointer + info-content only in v1.
//   - JSON mode: --from-json <path|-> reads a full Entry JSON. The
//     agent-write path; supports command-kind with concerns.
//
// In both modes, --id is optional (a ULID is generated if omitted) and
// the new entry's id is printed to stdout.
func RunAdd(opts AddOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex add: store root not set (use DEX_STORE)")
		return 2
	}

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	parent := fs.String("parent", "", "ULID of the rolodex to add to (required)")
	fromJSON := fs.String("from-json", "", "path to a JSON entry file, or '-' for stdin")
	id := fs.String("id", "", "ULID for the new entry (default: generated)")
	slug := fs.String("slug", "", "slug for the new entry (flag mode)")
	label := fs.String("label", "", "label for the new entry (flag mode)")
	context := fs.String("context", "", "optional context string")
	kind := fs.String("kind", "", "pointer | info (flag mode)")
	pointerTo := fs.String("pointer-to", "", "target ULID (when --kind=pointer)")
	content := fs.String("content", "", "info content (when --kind=info)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	if *parent == "" {
		fmt.Fprintln(opts.Stderr, "dex add: --parent is required")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex add: %v\n", err)
		return 1
	}

	parentR, ok, err := s.LookupByID(*parent)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex add: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex add: parent rolodex %q not found\n", *parent)
		return 1
	}

	var entry model.Entry
	if *fromJSON != "" {
		entry, err = readEntryJSON(*fromJSON, opts.Stderr)
		if err != nil {
			return 1
		}
	} else {
		entry, err = buildEntryFromFlags(*slug, *label, *context, *kind, *pointerTo, *content, opts.Stderr)
		if err != nil {
			return 2
		}
	}

	// Assign or generate the id.
	if *id != "" {
		entry.ID = *id
	}
	if entry.ID == "" {
		entry.ID = newULID()
	}

	// Reject duplicate slug in the parent.
	for _, e := range parentR.Entries {
		if e.Slug == entry.Slug {
			fmt.Fprintf(opts.Stderr,
				"dex add: parent already has an entry with slug %q (use 'dex edit' to modify)\n",
				entry.Slug)
			return 1
		}
	}

	parentR.Entries = append(parentR.Entries, entry)
	if err := s.WriteRolodex(parentR); err != nil {
		fmt.Fprintf(opts.Stderr, "dex add: %v\n", err)
		return 1
	}

	fmt.Fprintln(opts.Stdout, entry.ID)
	return 0
}

func buildEntryFromFlags(slug, label, context, kind, pointerTo, content string, stderr io.Writer) (model.Entry, error) {
	if slug == "" || label == "" || kind == "" {
		fmt.Fprintln(stderr, "dex add: --slug, --label, and --kind are required in flag mode")
		return model.Entry{}, fmt.Errorf("missing required flags")
	}
	entry := model.Entry{
		NodeCore: model.NodeCore{Slug: slug, Label: label, Context: context},
		Kind:     model.EntryKind(kind),
	}
	switch model.EntryKind(kind) {
	case model.KindPointer:
		if pointerTo == "" {
			fmt.Fprintln(stderr, "dex add: --pointer-to is required when --kind=pointer")
			return model.Entry{}, fmt.Errorf("missing --pointer-to")
		}
		entry.Pointer = &model.PointerPayload{To: pointerTo}
	case model.KindInfo:
		if content == "" {
			fmt.Fprintln(stderr, "dex add: --content is required when --kind=info (provider mode not supported in flag-based add)")
			return model.Entry{}, fmt.Errorf("missing --content")
		}
		entry.Info = &model.InfoPayload{Content: content}
	case model.KindCommand:
		fmt.Fprintln(stderr, "dex add: --kind=command not supported in flag mode; use --from-json")
		return model.Entry{}, fmt.Errorf("command kind requires --from-json")
	default:
		fmt.Fprintf(stderr, "dex add: unknown kind %q (want pointer or info)\n", kind)
		return model.Entry{}, fmt.Errorf("unknown kind")
	}
	return entry, nil
}

func readEntryJSON(pathOrDash string, stderr io.Writer) (model.Entry, error) {
	var b []byte
	var err error
	if pathOrDash == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(pathOrDash)
	}
	if err != nil {
		fmt.Fprintf(stderr, "dex add: read entry json: %v\n", err)
		return model.Entry{}, err
	}
	var entry model.Entry
	if err := json.Unmarshal(b, &entry); err != nil {
		fmt.Fprintf(stderr, "dex add: parse entry json: %v\n", err)
		return model.Entry{}, err
	}
	return entry, nil
}

// newULID generates a fresh ULID using crypto/rand entropy. We use the
// stdlib + ulid package here rather than calling out to a system tool so
// the binary stays self-contained.
func newULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy{}).String()
}

// ulidEntropy is a tiny adapter so ulid.MustNew accepts crypto/rand directly.
type ulidEntropy struct{}

func (ulidEntropy) Read(p []byte) (int, error) { return rand.Read(p) }

// Suppress unused-import warning for strings in case future edits need it.
var _ = strings.HasPrefix
```

(Note: `_ = strings.HasPrefix` is a placeholder so the file imports `strings` — drop it if not needed.)

Add the `oklog/ulid` dependency:
```bash
go get github.com/oklog/ulid/v2
```

Modify `cmd/dex/main.go`:
```go
	case "add":
		os.Exit(runAdd(os.Args[2:]))
```

Helper:
```go
func runAdd(args []string) int {
	return cli.RunAdd(cli.AddOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
	}, args)
}
```

(Note: `RunAdd` does its own flag parsing because of the many add-specific flags. Don't pre-parse here.)

Add to usage:
```
  add --parent <uuid> {--slug ... --label ... --kind ... <kind-specific>}|{--from-json <path|->}
                         Add an entry to a rolodex. Auto-generates the
                         new entry's ULID and prints it to stdout.
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — all 6 add tests + everything prior.

- [ ] **Step 5: Smoke test**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-add-smoke
mkdir -p /tmp/dex-add-smoke/{bundled,personal,private,ephemeral}
cat > /tmp/dex-add-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": []
}
JSON

# Flag-mode info add
DEX_STORE=/tmp/dex-add-smoke ./dex add \
  --parent 01HB00000000000000000000R1 \
  --slug readme --label Readme --kind info --content "first content"

# Flag-mode pointer add
DEX_STORE=/tmp/dex-add-smoke ./dex add \
  --parent 01HB00000000000000000000R1 \
  --slug tools --label Tools --kind pointer \
  --pointer-to 01HB00000000000000000000T1

# JSON-mode command add
cat > /tmp/dex-add-smoke/cmd.json <<'JSON'
{
  "slug": "greet",
  "label": "Greet",
  "kind": "command",
  "command": {
    "template": "echo hello, {who}",
    "concerns": [{
      "local_id": "who",
      "slug": "who-concern",
      "label": "Who",
      "required": true,
      "strict": false
    }]
  }
}
JSON
# Note: the JSON above omits ids on the entry and concern; the entry id
# gets auto-generated. The concern id, however, is schema-required. This
# is a known edge: for v1, callers using --from-json should populate
# concern ids themselves. Add ids for the smoke test:
cat > /tmp/dex-add-smoke/cmd.json <<'JSON'
{
  "id": "01HB00000000000000000000C1",
  "slug": "greet",
  "label": "Greet",
  "kind": "command",
  "command": {
    "template": "echo hello, {who}",
    "concerns": [{
      "id": "01HB00000000000000000000K1",
      "local_id": "who",
      "slug": "who-concern",
      "label": "Who",
      "required": true,
      "strict": false
    }]
  }
}
JSON
DEX_STORE=/tmp/dex-add-smoke ./dex add --parent 01HB00000000000000000000R1 --from-json /tmp/dex-add-smoke/cmd.json

# Verify all three entries landed
DEX_STORE=/tmp/dex-add-smoke ./dex ls
rm -rf /tmp/dex-add-smoke
```

Expected: each `add` prints a ULID; `dex ls` shows three entries (readme, tools, greet).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/add.go internal/cli/add_test.go cmd/dex/main.go go.mod go.sum
git commit -m "$(cat <<'EOF'
Implement dex add (flag + --from-json modes)

Adds an entry to an existing parent rolodex. Flag mode covers pointer
+ info-content (the simple kinds); --from-json covers command-kind
with concerns and any future complex shapes — this is the path agents
use after assembling an entry from explored examples. ULID is
auto-generated if --id is omitted; the new entry's id is printed to
stdout for shell/agent pipelines.
EOF
)"
```

---

## Task 3: `dex edit`

Modify mutable fields on an existing entry. Targets the entry by its ULID (`dex edit <entry-uuid>`) and accepts per-field flags. Editable in v1: `--label`, `--context`, plus kind-specific payloads (`--content`, `--pointer-to`). Concern editing on commands is deferred.

**Files:**
- Create: `internal/cli/edit.go`
- Create: `internal/cli/edit_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/edit_test.go`:
```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writeEditFixture(t *testing.T, root string) (entryID string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	rolodex := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000E1",
				"slug": "readme",
				"label": "Old label",
				"kind": "info",
				"info": { "content": "old content" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000E1"
}

func TestEditLabelAndContext(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeEditFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{entryID, "--label", "New label", "--context", "new context"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			Label   string `json:"label"`
			Context string `json:"context"`
		} `json:"entries"`
	}
	_ = json.Unmarshal(b, &got)
	if got.Entries[0].Label != "New label" || got.Entries[0].Context != "new context" {
		t.Fatalf("fields not updated: %+v", got.Entries)
	}
}

func TestEditInfoContent(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeEditFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out},
		[]string{entryID, "--content", "new content"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	if !strings.Contains(string(b), "new content") {
		t.Fatalf("content not updated; rolodex: %s", string(b))
	}
}

func TestEditEntryNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeEditFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ", "--label", "x"})
	if exit == 0 {
		t.Fatal("expected error for unknown entry")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestEditRequiresEntryID(t *testing.T) {
	tmp := t.TempDir()
	writeEditFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"--label", "x"})
	if exit == 0 {
		t.Fatal("expected error when entry id is missing")
	}
}

func TestEditWrongPayloadFlagForKind(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeEditFixture(t, tmp)
	// The fixture entry is info kind; --pointer-to should be rejected.
	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{entryID, "--pointer-to", "01HB00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected error for kind/flag mismatch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunEdit` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/edit.go`:
```go
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type EditOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunEdit implements `dex edit <entry-ULID> [flags...]`. Editable fields:
// --label, --context, plus kind-specific (--content for info,
// --pointer-to for pointer). Concern editing on commands is deferred.
func RunEdit(opts EditOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex edit: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) < 1 {
		fmt.Fprintln(opts.Stderr, "dex edit: first argument must be the entry ULID")
		return 2
	}
	entryID := argv[0]

	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	label := fs.String("label", "", "new label")
	context := fs.String("context", "", "new context")
	content := fs.String("content", "", "new info content (info kind only)")
	pointerTo := fs.String("pointer-to", "", "new pointer target (pointer kind only)")
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}

	labelSet := isFlagSet(fs, "label")
	contextSet := isFlagSet(fs, "context")
	contentSet := isFlagSet(fs, "content")
	pointerSet := isFlagSet(fs, "pointer-to")

	if !labelSet && !contextSet && !contentSet && !pointerSet {
		fmt.Fprintln(opts.Stderr, "dex edit: at least one field flag must be set")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex edit: %v\n", err)
		return 1
	}
	entry, parent, ok, err := s.LookupEntryByID(entryID)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex edit: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex edit: entry %q not found\n", entryID)
		return 1
	}

	if contentSet && entry.Kind != model.KindInfo {
		fmt.Fprintf(opts.Stderr, "dex edit: --content only applies to info entries (got %s)\n", entry.Kind)
		return 2
	}
	if pointerSet && entry.Kind != model.KindPointer {
		fmt.Fprintf(opts.Stderr, "dex edit: --pointer-to only applies to pointer entries (got %s)\n", entry.Kind)
		return 2
	}

	// Apply edits.
	if labelSet {
		entry.Label = *label
	}
	if contextSet {
		entry.Context = *context
	}
	if contentSet {
		if entry.Info == nil {
			entry.Info = &model.InfoPayload{}
		}
		entry.Info.Content = *content
	}
	if pointerSet {
		entry.Pointer = &model.PointerPayload{To: *pointerTo}
	}

	// Splice the edited entry back into the parent's entries slice.
	for i, e := range parent.Entries {
		if e.ID == entryID {
			parent.Entries[i] = entry
			break
		}
	}

	if err := s.WriteRolodex(parent); err != nil {
		fmt.Fprintf(opts.Stderr, "dex edit: %v\n", err)
		return 1
	}
	return 0
}

// isFlagSet reports whether the named flag was explicitly set on the
// flag set (as distinct from being left at its zero default). The Go
// flag package doesn't track this directly, so we walk Visit.
func isFlagSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}
```

Modify `cmd/dex/main.go`:
```go
	case "edit":
		os.Exit(runEdit(os.Args[2:]))
```

Helper:
```go
func runEdit(args []string) int {
	return cli.RunEdit(cli.EditOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
	}, args)
}
```

Add to usage:
```
  edit <entry-uuid> [--label "..."] [--context "..."]
                   [--content "..."] [--pointer-to <uuid>]
                         Modify an existing entry's mutable fields.
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — 5 new edit tests plus everything prior.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/edit.go internal/cli/edit_test.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex edit (mutable-field updates by entry ULID)

Editable in v1: label, context, info.content, pointer.to. Kind/flag
mismatches error explicitly (--content on a pointer entry, etc.).
Concern editing on commands is deferred — a focused 'dex concern' slice
will own that surface.
EOF
)"
```

---

## Task 4: `dex rm`

Remove an entry from its parent rolodex by ULID. Tombstones are deferred — the entry is just spliced out of the `entries` array. If any other entry has a `pointer.to` or `concern.rolodex.to` targeting the removed entry, `dex doctor` will surface the dangling reference in Task 6.

**Files:**
- Create: `internal/cli/rm.go`
- Create: `internal/cli/rm_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/rm_test.go`:
```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writeRmFixture(t *testing.T, root string) (entryID string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	rolodex := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"keep","label":"keep","kind":"info","info":{"content":"a"}},
			{"id":"01HB00000000000000000000E2","slug":"remove","label":"remove","kind":"info","info":{"content":"b"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000E2"
}

func TestRmEntry(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeRmFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunRm(cli.RmOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{entryID})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"entries"`
	}
	_ = json.Unmarshal(b, &got)
	if len(got.Entries) != 1 {
		t.Fatalf("expected 1 remaining entry, got %d", len(got.Entries))
	}
	if got.Entries[0].Slug != "keep" {
		t.Fatalf("kept the wrong entry: %+v", got.Entries)
	}
}

func TestRmEntryNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeRmFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunRm(cli.RmOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected error for unknown entry")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestRmRequiresEntryID(t *testing.T) {
	tmp := t.TempDir()
	writeRmFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunRm(cli.RmOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("expected error when entry id is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunRm` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/rm.go`:
```go
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/store"
)

type RmOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunRm implements `dex rm <entry-ULID>`. Splices the entry out of its
// parent rolodex and writes the result. Pointers in other rolodexes
// that target the removed entry become dangling — `dex doctor` will
// surface those.
func RunRm(opts RmOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex rm: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) != 1 {
		fmt.Fprintln(opts.Stderr, "dex rm: requires exactly one entry ULID argument")
		return 2
	}
	entryID := argv[0]

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex rm: %v\n", err)
		return 1
	}
	_, parent, ok, err := s.LookupEntryByID(entryID)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex rm: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex rm: entry %q not found\n", entryID)
		return 1
	}

	filtered := parent.Entries[:0]
	for _, e := range parent.Entries {
		if e.ID != entryID {
			filtered = append(filtered, e)
		}
	}
	parent.Entries = filtered

	if err := s.WriteRolodex(parent); err != nil {
		fmt.Fprintf(opts.Stderr, "dex rm: %v\n", err)
		return 1
	}
	return 0
}
```

Modify `cmd/dex/main.go`:
```go
	case "rm":
		os.Exit(runRm(os.Args[2:]))
```

Helper:
```go
func runRm(args []string) int {
	return cli.RunRm(cli.RmOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
	}, args)
}
```

Add to usage:
```
  rm <entry-uuid>        Remove an entry from its parent rolodex.
                         Dangling pointers (if any) are surfaced by
                         `dex doctor`.
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — 3 new rm tests + everything prior.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/rm.go internal/cli/rm_test.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex rm (entry removal by ULID)

Splices the entry out of its parent rolodex's entries array; no
tombstone in v1. Pointers in other rolodexes targeting the removed
entry become dangling — `dex doctor` will surface those. Whole-rolodex
removal is deferred to a focused slice.
EOF
)"
```

---

## Task 5: `dex promote`

Move a rolodex from one visibility tier to another, preserving its ULID. The on-disk file moves between tier directories; the rolodex's `visibility` field is rewritten to match.

**Files:**
- Create: `internal/cli/promote.go`
- Create: `internal/cli/promote_test.go`
- Modify: `cmd/dex/main.go`
- Modify: `internal/store/write.go` (add a delete helper)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/promote_test.go`:
```go
package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writePromoteFixture(t *testing.T, root string) (rolodexID string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	// Ephemeral rolodex to be promoted to personal.
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "scratch",
		"label": "Scratch",
		"visibility": "ephemeral",
		"entries": []
	}`
	if err := os.WriteFile(filepath.Join(root, "ephemeral", "scratch.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000R1"
}

func TestPromoteToPersonal(t *testing.T) {
	tmp := t.TempDir()
	rolodexID := writePromoteFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{rolodexID, "--to", "personal"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	// Old location should be gone.
	if _, err := os.Stat(filepath.Join(tmp, "ephemeral", "scratch.json")); !os.IsNotExist(err) {
		t.Fatal("source file still exists after promote")
	}
	// New location should have a file.
	personalFiles, _ := filepath.Glob(filepath.Join(tmp, "personal", "*.json"))
	if len(personalFiles) != 1 {
		t.Fatalf("expected 1 file in personal/, got %v", personalFiles)
	}
	b, _ := os.ReadFile(personalFiles[0])
	if !strings.Contains(string(b), `"visibility": "personal"`) {
		t.Fatalf("visibility not rewritten in moved file: %s", string(b))
	}
}

func TestPromoteRolodexNotFound(t *testing.T) {
	tmp := t.TempDir()
	writePromoteFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ", "--to", "personal"})
	if exit == 0 {
		t.Fatal("expected error for unknown rolodex")
	}
}

func TestPromoteInvalidTier(t *testing.T) {
	tmp := t.TempDir()
	rolodexID := writePromoteFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{rolodexID, "--to", "nonsense"})
	if exit == 0 {
		t.Fatal("expected error for invalid tier")
	}
}

func TestPromoteRequiresArgs(t *testing.T) {
	tmp := t.TempDir()
	writePromoteFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("expected error when args are missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunPromote` undefined; `Store.DeleteRolodexFile` undefined.

- [ ] **Step 3: Add the delete helper**

Append to `internal/store/write.go`:
```go
// DeleteRolodexFile removes the on-disk file for the rolodex with the
// given id in the given tier. No-op if no matching file is found.
// Used by `dex promote` after writing the rolodex to its new tier.
func (s *Store) DeleteRolodexFile(v model.Visibility, id string) error {
	dir, ok := s.tiers[v]
	if !ok {
		return fmt.Errorf("store: unknown visibility %q", v)
	}
	path, err := findFileForID(dir, id)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	return os.Remove(path)
}
```

- [ ] **Step 4: Write the implementation**

Create `internal/cli/promote.go`:
```go
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type PromoteOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunPromote implements `dex promote <rolodex-ULID> --to <tier>`. Moves
// the rolodex's file between tier directories and rewrites its
// `visibility` field. The ULID is preserved, so backlinks survive.
func RunPromote(opts PromoteOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex promote: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) < 1 {
		fmt.Fprintln(opts.Stderr, "dex promote: first argument must be the rolodex ULID")
		return 2
	}
	rolodexID := argv[0]

	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	to := fs.String("to", "", "target visibility (bundled|personal|private|ephemeral)")
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}
	if *to == "" {
		fmt.Fprintln(opts.Stderr, "dex promote: --to is required")
		return 2
	}
	target := model.Visibility(*to)
	if err := target.Validate(); err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: %v\n", err)
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: %v\n", err)
		return 1
	}
	r, ok, err := s.LookupByID(rolodexID)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex promote: rolodex %q not found\n", rolodexID)
		return 1
	}
	if r.Visibility == target {
		fmt.Fprintf(opts.Stderr, "dex promote: rolodex %q is already in tier %s\n", rolodexID, target)
		return 0
	}

	origin := r.Visibility
	r.Visibility = target

	// Write to new tier first, then delete from old. Order matters: if
	// the write fails, the original file remains untouched.
	if err := s.WriteRolodex(r); err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: write to %s failed: %v\n", target, err)
		return 1
	}
	if err := s.DeleteRolodexFile(origin, rolodexID); err != nil {
		fmt.Fprintf(opts.Stderr,
			"dex promote: WARNING: wrote to %s but failed to delete from %s: %v (manual cleanup needed)\n",
			target, origin, err)
		return 1
	}
	return 0
}
```

Modify `cmd/dex/main.go`:
```go
	case "promote":
		os.Exit(runPromote(os.Args[2:]))
```

Helper:
```go
func runPromote(args []string) int {
	return cli.RunPromote(cli.PromoteOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
	}, args)
}
```

Add to usage:
```
  promote <rolodex-uuid> --to <bundled|personal|private|ephemeral>
                         Move a rolodex to a different visibility tier.
                         ULID is preserved so backlinks survive.
```

- [ ] **Step 5: Run tests**

Run: `go test ./...`
Expected: PASS — 4 new promote tests + everything prior.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/promote.go internal/cli/promote_test.go internal/store/write.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex promote (move rolodex between visibility tiers)

ULID is preserved so backlinks survive. Write-to-new-then-delete-from-old
ordering means a failed write leaves the original untouched; a failed
delete after a successful write surfaces a warning (manual cleanup hint).
Also adds Store.DeleteRolodexFile as the file-removal counterpart to
WriteRolodex.
EOF
)"
```

---

## Task 6: `dex doctor`

Validate the entire store. Two checks in v1:
1. **Schema validity:** every JSON file in every tier loads + validates. Already enforced on read, but `doctor` makes it explicit and surfaces issues without requiring a verb that happens to touch each file.
2. **Dangling references:** every `pointer.to` and every concern's `rolodex.to` must resolve to a rolodex that exists in the store.

Output: a human-readable list of findings, exit 0 if clean and 1 if any issue. JSON output later if needed.

**Files:**
- Create: `internal/cli/doctor.go`
- Create: `internal/cli/doctor_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/doctor_test.go`:
```go
package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writeDoctorCleanFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	// Two rolodexes; the second is the target of the first's pointer.
	r1 := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"tools","label":"Tools","kind":"pointer","pointer":{"to":"01HB00000000000000000000T1"}}
		]
	}`
	r2 := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000T1",
		"slug": "tools-collection",
		"label": "Tools",
		"visibility": "bundled",
		"entries": []
	}`
	_ = os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(r1), 0o644)
	_ = os.WriteFile(filepath.Join(root, "bundled", "tools.json"), []byte(r2), 0o644)
}

func writeDoctorDanglingFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	// One rolodex with a pointer to a non-existent target.
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"orphan","label":"Orphan","kind":"pointer","pointer":{"to":"01HB00000000000000000000ZZ"}}
		]
	}`
	_ = os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(r), 0o644)
}

func TestDoctorCleanStore(t *testing.T) {
	tmp := t.TempDir()
	writeDoctorCleanFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunDoctor(cli.DoctorOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit != 0 {
		t.Fatalf("clean store should exit 0; exit=%d stderr=%q", exit, errBuf.String())
	}
	if !strings.Contains(out.String(), "clean") && !strings.Contains(out.String(), "OK") && !strings.Contains(out.String(), "no issues") {
		t.Fatalf("expected positive output for clean store; got %q", out.String())
	}
}

func TestDoctorDanglingPointer(t *testing.T) {
	tmp := t.TempDir()
	writeDoctorDanglingFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunDoctor(cli.DoctorOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("dangling pointer should produce non-zero exit")
	}
	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "dangling") && !strings.Contains(combined, "ZZ") {
		t.Fatalf("expected dangling-pointer mention in output; got out=%q stderr=%q", out.String(), errBuf.String())
	}
}

func TestDoctorEmptyStore(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	var out bytes.Buffer
	exit := cli.RunDoctor(cli.DoctorOpts{StoreRoot: tmp, Stdout: &out}, nil)
	if exit != 0 {
		t.Fatalf("empty store should exit 0; exit=%d", exit)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunDoctor` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/doctor.go`:
```go
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type DoctorOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunDoctor implements `dex doctor`. Walks every rolodex via LoadAll
// (which itself schema-validates each file on read), then scans for
// dangling references — any pointer.to or concern.rolodex.to that
// names a rolodex ULID we haven't seen.
//
// Exit 0 if the store is clean; exit 1 if any issue is reported.
func RunDoctor(opts DoctorOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex doctor: store root not set (use DEX_STORE)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex doctor: %v\n", err)
		return 1
	}
	rolodexes, err := s.LoadAll()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex doctor: %v\n", err)
		return 1
	}

	known := map[string]bool{}
	for _, r := range rolodexes {
		known[r.ID] = true
	}

	var findings []string
	for _, r := range rolodexes {
		for _, e := range r.Entries {
			if e.Kind == model.KindPointer && e.Pointer != nil {
				if !known[e.Pointer.To] {
					findings = append(findings,
						fmt.Sprintf("dangling pointer: rolodex %s/%s entry %s/%s → %s",
							r.Visibility, r.Slug, e.Slug, e.ID, e.Pointer.To))
				}
			}
			if e.Kind == model.KindCommand && e.Command != nil {
				for _, c := range e.Command.Concerns {
					if c.Rolodex != nil && !known[c.Rolodex.To] {
						findings = append(findings,
							fmt.Sprintf("dangling concern rolodex: rolodex %s/%s entry %s/%s concern %s → %s",
								r.Visibility, r.Slug, e.Slug, e.ID, c.LocalID, c.Rolodex.To))
					}
				}
			}
		}
	}

	if len(findings) == 0 {
		fmt.Fprintf(opts.Stdout, "dex doctor: store is clean (%d rolodexes checked, no issues)\n", len(rolodexes))
		return 0
	}
	fmt.Fprintf(opts.Stderr, "dex doctor: %d issue(s) found:\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(opts.Stderr, "  - %s\n", f)
	}
	return 1
}
```

Modify `cmd/dex/main.go`:
```go
	case "doctor":
		os.Exit(runDoctor(os.Args[2:]))
```

Helper:
```go
func runDoctor(args []string) int {
	return cli.RunDoctor(cli.DoctorOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
	}, args)
}
```

Add to usage:
```
  doctor                 Validate the store: schema check + dangling
                         pointer/concern-rolodex detection.
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — 3 new doctor tests + everything prior.

- [ ] **Step 5: Smoke test**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-doctor-smoke
mkdir -p /tmp/dex-doctor-smoke/{bundled,personal,private,ephemeral}
# Clean store
cat > /tmp/dex-doctor-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": []
}
JSON
DEX_STORE=/tmp/dex-doctor-smoke ./dex doctor

# Add a dangling pointer
cat > /tmp/dex-doctor-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": [
    {"id":"01HB00000000000000000000E1","slug":"orphan","label":"Orphan","kind":"pointer","pointer":{"to":"01HB00000000000000000000ZZ"}}
  ]
}
JSON
DEX_STORE=/tmp/dex-doctor-smoke ./dex doctor
echo "exit=$?"
rm -rf /tmp/dex-doctor-smoke
```

Expected: first call exits 0 with a "clean" message; second exits 1 with a dangling-pointer finding.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex doctor (schema + dangling-reference check)

Walks every rolodex via LoadAll (schema validation happens on read) and
scans for dangling pointer.to / concern.rolodex.to references. Exit 0
when clean, exit 1 with a findings list otherwise. Useful for catching
the state mutations like dex rm and dex promote can leave behind, plus
schema violations that snuck in via hand-edits.
EOF
)"
```

---

## Self-Review

**Spec coverage** (against the design's CLI section + Phased Delivery P-11.8):
- `dex add --parent <uuid> ...` — Task 2 ✓
- `dex edit <uuid> --field=value` — Task 3 ✓
- `dex rm <uuid>` — Task 4 ✓
- `dex promote <uuid> --to <tier>` — Task 5 ✓
- `dex doctor` — Task 6 ✓
- Architect landmine #2 (per-rolodex write lock + atomic rename) — Task 1 ✓

**Out of scope explicitly:**
- Creating new rolodexes from scratch
- Concern-level mutations (need their own slice)
- Tombstones for deletions
- `dex tree` (independent of writes; defer)
- `dex add --new-rolodex` mode

**Placeholder scan:** none. Every step has working code and explicit commands. The `_ = strings.HasPrefix` line in Task 2's `add.go` is an intentional unused-import suppressor; the implementer can drop it if `strings` isn't actually referenced in the final file.

**Type consistency:**
- All `Run<Verb>(opts, argv) int` signatures consistent with the established pattern.
- `AddOpts`, `EditOpts`, `RmOpts`, `PromoteOpts`, `DoctorOpts` all carry the same `StoreRoot/Stdout/Stderr` shape (no `JSON` flag — mutations don't have a structured-output mode in v1).
- `Store.WriteRolodex(r) error` and `Store.DeleteRolodexFile(v, id) error` are the two new store methods.
- `isFlagSet(fs, name) bool` introduced in Task 3 (`edit.go`); not reused elsewhere — single-use is fine.
- `newULID()` and the `ulidEntropy` type are private to `add.go` in Task 2.

**Three things to watch during execution:**

1. **The `_ = strings.HasPrefix` line in `add.go`** is a workaround for an unused `strings` import I included defensively. Drop the line (and the `strings` import) if the final file doesn't reference `strings`. If a later step in this plan ends up needing it, restore.

2. **The `ulid.MustNew` API** in `github.com/oklog/ulid/v2` may have minor signature differences from what's written. If the implementer hits an API mismatch, run `go doc github.com/oklog/ulid/v2.MustNew` and adjust. The intent is: ULID from current time + crypto/rand entropy. Worst case: write a 26-char Crockford base32 generator inline using `crypto/rand` directly — it's ~20 lines of code.

3. **The concurrent-write test in Task 1** (`TestWriteRolodexAtomicOnConcurrentWrites`) uses `O_CREATE|O_EXCL` lockfiles, which on macOS/Linux are atomic enough that two simultaneous attempts will reliably have one succeed and others fail-fast with "file exists." If the test is flaky in some environments, the fix is to switch to `flock(2)` via `golang.org/x/sys/unix` — but try the simple form first.
