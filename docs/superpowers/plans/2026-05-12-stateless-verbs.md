# Stateless Verbs Implementation Plan (`explore`, `search`, `activate`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the three remaining stateless verbs from the P-11.2 design — `dex explore`, `dex search`, `dex activate` — so the CLI surface unblocks agent workflows independently of the session API.

**Architecture:** Each verb gets its own file under `internal/cli/` mirroring the `ls.go` pattern (a `Run<Verb>(opts, argv) int` entry point with injectable IO). Two new store helpers — `LoadAll()` (walk every tier) and `LookupEntryByID()` (find an entry across all rolodexes) — give the verbs the lookup surface they need without leaking store internals. `dex activate` for commands shells out via `sh -c` after template substitution; a `--dry-run` flag prints the assembled command without executing. `dex search` does case-insensitive substring matching over slug/label/context/explore.description (per the design's "Substring v1; revisit if it feels limp").

**Tech Stack:** Go 1.22+ stdlib only. Reuses `internal/model`, `internal/path`, `internal/store`. No new external dependencies.

---

## Out of scope (deliberately deferred)

- **`info.provider` execution** — `dex activate` on an info entry with `provider:` set will return an explicit "providers not implemented" error. Provider runtime contract is a P-11.4 / session-API decision per the architect's response.
- **Concern `validator` enforcement** — values are accepted as-is; the validator script-id is honored only as a placeholder. Future work wires this in.
- **`strict: true` enforcement on concerns** — same as validator; v1 trust-but-don't-verify.
- **`depends_on` resolution** — relevant only when suggestion providers exist; defer.
- **Path reconstruction in search output** — search returns parent rolodex id/slug + entry, not a fully-reconstructed `/foo/bar` path. Doable later via a graph walk; not needed for v1 utility.
- **Fuzzy / FTS5 search** — design defers this to "when it feels limp."

---

## File Structure

```
dex/
├── internal/
│   ├── cli/
│   │   ├── explore.go         Task 2
│   │   ├── explore_test.go    Task 2
│   │   ├── search.go          Task 4
│   │   ├── search_test.go     Task 4
│   │   ├── activate.go        Tasks 5, 6, 7
│   │   └── activate_test.go   Tasks 5, 6, 7
│   └── store/
│       ├── store.go           Tasks 1, 3 (append)
│       └── store_test.go      Tasks 1, 3 (append)
└── cmd/dex/
    └── main.go                Tasks 2, 4, 5 (verb dispatch wiring)
```

`internal/cli/ls.go` is unchanged. Each verb's wiring keeps the pattern that worked for `ls`: `Run<Verb>(opts, argv) int` with stdout/stderr injection.

---

## Task 1: Add `Store.LookupEntryByID`

Used by `dex explore <ULID>` and `dex activate <ULID>` to find an entry across all loaded rolodexes by id. Returns the entry + its parent rolodex (so callers can present context). Pure addition; no behavior change to existing methods.

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:
```go
func TestLookupEntryByID(t *testing.T) {
	s, err := store.Open("testdata/simple")
	if err != nil {
		t.Fatal(err)
	}
	// testdata/simple's bundled root contains an entry with id 01HQ7AB000000000000000ENT1.
	entry, parent, ok, err := s.LookupEntryByID("01HQ7AB000000000000000ENT1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected to find the bundled root's tools entry")
	}
	if entry.Slug != "tools" {
		t.Fatalf("entry.slug: got %q want tools", entry.Slug)
	}
	if parent.Slug != "root" {
		t.Fatalf("parent.slug: got %q want root", parent.Slug)
	}
}

func TestLookupEntryByIDMissing(t *testing.T) {
	s, err := store.Open("testdata/simple")
	if err != nil {
		t.Fatal(err)
	}
	_, _, ok, err := s.LookupEntryByID("01HQ7AB000000000000000ZZZZ")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if ok {
		t.Fatal("expected not-found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL — `LookupEntryByID` undefined.

- [ ] **Step 3: Append implementation to `internal/store/store.go`**

```go
// LookupEntryByID scans every tier (including ephemeral) for an entry
// with the given ID. The second return is the parent rolodex; the third
// is false when not found. Errors are reserved for IO/schema failures.
func (s *Store) LookupEntryByID(id string) (model.Entry, model.Rolodex, bool, error) {
	for _, v := range []model.Visibility{
		model.VisibilityBundled,
		model.VisibilityPersonal,
		model.VisibilityPrivate,
		model.VisibilityEphemeral,
	} {
		rolodexes, err := s.LoadTier(v)
		if err != nil {
			return model.Entry{}, model.Rolodex{}, false, err
		}
		for _, r := range rolodexes {
			for _, e := range r.Entries {
				if e.ID == id {
					return e, r, true, nil
				}
			}
		}
	}
	return model.Entry{}, model.Rolodex{}, false, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/...`
Expected: PASS — both new tests plus all prior store tests.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "$(cat <<'EOF'
Add Store.LookupEntryByID

Used by explore + activate to look up an entry across all rolodexes by
its global ULID. Returns the entry and its containing rolodex so callers
can present context.
EOF
)"
```

---

## Task 2: `dex explore` Verb

Resolves a single entry by ULID or path and returns its structured self-description — the `explore` block plus declared concerns for command-kind entries. JSON-friendly: agents read this to learn how to invoke an entry.

**Files:**
- Create: `internal/cli/explore.go`
- Create: `internal/cli/explore_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/explore_test.go`:
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

// writeExploreFixture sets up a store containing one rolodex with three
// entries: a command (with concerns), an info, and a pointer.
func writeExploreFixture(t *testing.T, root string) {
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
		"label": "Bundled root",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000CM1",
				"slug": "broker-status",
				"label": "Broker status",
				"kind": "command",
				"command": {
					"template": "wm broker status --provider {provider}",
					"concerns": [
						{
							"id": "01HB00000000000000000000CN1",
							"local_id": "provider",
							"slug": "provider-concern",
							"label": "Which provider?",
							"required": true,
							"strict": false
						}
					]
				},
				"explore": {
					"description": "Snapshot of provider freshness.",
					"examples": [
						{"description": "all", "invocation": "wm broker status"}
					],
					"notes": "non-zero exit if any provider is stale"
				}
			},
			{
				"id": "01HB00000000000000000000IN1",
				"slug": "readme",
				"label": "Readme",
				"kind": "info",
				"info": { "content": "hi" }
			},
			{
				"id": "01HB00000000000000000000PT1",
				"slug": "tools",
				"label": "Tools",
				"kind": "pointer",
				"pointer": { "to": "01HB00000000000000000000T1" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExploreByULIDCommandKind(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"01HB00000000000000000000CM1"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}

	var got struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		Kind     string `json:"kind"`
		Explore  struct {
			Description string `json:"description"`
			Notes       string `json:"notes"`
		} `json:"explore"`
		Concerns []struct {
			LocalID  string `json:"local_id"`
			Required bool   `json:"required"`
		} `json:"concerns"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v raw=%s", err, out.String())
	}
	if got.Slug != "broker-status" {
		t.Fatalf("slug: got %q", got.Slug)
	}
	if got.Kind != "command" {
		t.Fatalf("kind: got %q", got.Kind)
	}
	if got.Explore.Description == "" {
		t.Fatal("explore.description missing")
	}
	if len(got.Concerns) != 1 || got.Concerns[0].LocalID != "provider" {
		t.Fatalf("concerns: %+v", got.Concerns)
	}
}

func TestExploreByPathInfoKind(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/readme"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got struct {
		Slug     string                   `json:"slug"`
		Kind     string                   `json:"kind"`
		Concerns []map[string]interface{} `json:"concerns"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Slug != "readme" || got.Kind != "info" {
		t.Fatalf("got %+v", got)
	}
	if len(got.Concerns) != 0 {
		t.Fatalf("non-command should have no concerns; got %+v", got.Concerns)
	}
}

func TestExploreByPathPointerKind(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out},
		[]string{"/tools"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "tools") {
		t.Fatalf("human output should mention 'tools'; got %q", out.String())
	}
}

func TestExploreNoArg(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("explore without arg should error")
	}
	if !strings.Contains(errBuf.String(), "argument") {
		t.Fatalf("expected 'argument' in stderr; got %q", errBuf.String())
	}
}

func TestExploreNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000XXXX"})
	// Wait — ULIDs are exactly 26 chars. The above is 27 (with the
	// trailing XXXX bringing it to 28). For a more correct invalid-id
	// test, use a 26-char id that doesn't exist:
	_ = exit
	exit = cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected non-zero for unknown ULID")
	}
}
```

(The TestExploreNotFound test is messy — clean it up to a single straight test in your implementation; just keep the working assertion.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunExplore`, `ExploreOpts` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/explore.go`:
```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/path"
	"github.com/scshafe/dex/internal/store"
)

type ExploreOpts struct {
	StoreRoot string
	JSON      bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// exploreOutput is the structured payload `dex explore` emits. It's
// stable enough that agents can rely on it.
type exploreOutput struct {
	ID          string           `json:"id"`
	Slug        string           `json:"slug"`
	Label       string           `json:"label"`
	Kind        model.EntryKind  `json:"kind"`
	Context     string           `json:"context,omitempty"`
	Explore     *model.Explore   `json:"explore,omitempty"`
	Concerns    []model.Concern  `json:"concerns"`
	ParentSlug  string           `json:"parent_slug"`
	ParentID    string           `json:"parent_id"`
}

// RunExplore implements `dex explore <ULID|/path>`. Prints the entry's
// self-description (explore block + concerns for command kind).
func RunExplore(opts ExploreOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex explore: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) != 1 {
		fmt.Fprintln(opts.Stderr, "dex explore: requires exactly one argument (<ULID> or </path>)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
		return 1
	}

	arg := argv[0]
	var entry model.Entry
	var parent model.Rolodex

	if strings.HasPrefix(arg, "/") {
		root, err := s.MergedRoot()
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
			return 1
		}
		result, err := path.Resolve(s, root, arg)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
			return 1
		}
		entry = result.Entry
		parent = result.ParentRolodex
	} else {
		e, p, ok, err := s.LookupEntryByID(arg)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(opts.Stderr, "dex explore: entry %q not found\n", arg)
			return 1
		}
		entry = e
		parent = p
	}

	out := exploreOutput{
		ID:         entry.ID,
		Slug:       entry.Slug,
		Label:      entry.Label,
		Kind:       entry.Kind,
		Context:    entry.Context,
		Explore:    entry.Explore,
		Concerns:   []model.Concern{},
		ParentSlug: parent.Slug,
		ParentID:   parent.ID,
	}
	if entry.Kind == model.KindCommand && entry.Command != nil {
		out.Concerns = entry.Command.Concerns
	}

	if opts.JSON {
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: encode: %v\n", err)
			return 1
		}
		return 0
	}
	return emitExploreHuman(opts.Stdout, out, entry)
}

func emitExploreHuman(w io.Writer, out exploreOutput, entry model.Entry) int {
	fmt.Fprintf(w, "%s  [%s]\n", out.Slug, out.Kind)
	if out.Label != "" {
		fmt.Fprintf(w, "  label:   %s\n", out.Label)
	}
	if out.Context != "" {
		fmt.Fprintf(w, "  context: %s\n", out.Context)
	}
	fmt.Fprintf(w, "  id:      %s\n", out.ID)
	fmt.Fprintf(w, "  parent:  %s (%s)\n", out.ParentSlug, out.ParentID)
	if out.Explore != nil {
		if out.Explore.Description != "" {
			fmt.Fprintf(w, "\n%s\n", out.Explore.Description)
		}
		if len(out.Explore.Examples) > 0 {
			fmt.Fprintln(w, "\nExamples:")
			for _, ex := range out.Explore.Examples {
				fmt.Fprintf(w, "  # %s\n  %s\n", ex.Description, ex.Invocation)
			}
		}
		if out.Explore.Notes != "" {
			fmt.Fprintf(w, "\nNotes: %s\n", out.Explore.Notes)
		}
	}
	if entry.Kind == model.KindCommand && entry.Command != nil {
		fmt.Fprintf(w, "\nTemplate: %s\n", entry.Command.Template)
		if len(entry.Command.Concerns) > 0 {
			fmt.Fprintln(w, "Concerns:")
			for _, c := range entry.Command.Concerns {
				req := ""
				if c.Required {
					req = " (required)"
				}
				fmt.Fprintf(w, "  %s — %s%s\n", c.LocalID, c.Label, req)
			}
		}
	}
	return 0
}
```

Modify `cmd/dex/main.go` to add an `explore` case. The current dispatch (after Task 11 of the foundations slice) has cases for `ls` and `version`. Insert before `default:`:
```go
	case "explore":
		os.Exit(runExplore(os.Args[2:]))
```

And add `runExplore` after `runLs`:
```go
func runExplore(args []string) int {
	fs := flag.NewFlagSet("explore", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON instead of human output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return cli.RunExplore(cli.ExploreOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
		JSON:      *jsonOut,
	}, fs.Args())
}
```

Also extend the usage text — replace the `Verbs:` block in `usage()` with:
```
Verbs:
  ls [--json] [<uuid>|<path>]
                         List entries. With no arg, the merged root.
                         <uuid> looks up a rolodex directly.
                         <path> starts with "/" (e.g. "/tools" or
                         "/tools/hammer") and walks pointers.
  explore [--json] <uuid|path>
                         Print an entry's self-description (explore
                         block + concerns for command-kind).
  version                Print version
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — all new explore tests + everything prior.

- [ ] **Step 5: Smoke test**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-explore-smoke
mkdir -p /tmp/dex-explore-smoke/{bundled,personal,private,ephemeral}
cat > /tmp/dex-explore-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HB00000000000000000000C1",
      "slug": "echo-it",
      "label": "Echo",
      "kind": "command",
      "command": {
        "template": "echo {msg}",
        "concerns": [{
          "id": "01HB00000000000000000000K1",
          "local_id": "msg",
          "slug": "msg-concern",
          "label": "Message",
          "required": true,
          "strict": false
        }]
      },
      "explore": {
        "description": "Echo a message.",
        "examples": [{"description": "hello", "invocation": "echo hello"}]
      }
    }
  ]
}
JSON
DEX_STORE=/tmp/dex-explore-smoke ./dex explore /echo-it
DEX_STORE=/tmp/dex-explore-smoke ./dex explore --json /echo-it
DEX_STORE=/tmp/dex-explore-smoke ./dex explore 01HB00000000000000000000C1
rm -rf /tmp/dex-explore-smoke
```

Expected: each call prints the entry's structured details (human form for the first/third; JSON for the second).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/explore.go internal/cli/explore_test.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex explore (uuid or path → structured self-description)

Resolves an argument to a single entry and emits its explore block
plus, for command-kind entries, the declared concerns. Agents use the
--json form to learn an entry's invocation contract; humans get a
formatted human-readable variant by default.
EOF
)"
```

---

## Task 3: Add `Store.LoadAll`

Used by `dex search` to walk every rolodex across all tiers. Pure addition.

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:
```go
func TestLoadAll(t *testing.T) {
	s, err := store.Open("testdata/merge-precedence")
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.LoadAll()
	if err != nil {
		t.Fatalf("loadall: %v", err)
	}
	// merge-precedence fixture has 1 bundled root + 1 personal root = 2.
	if len(all) != 2 {
		t.Fatalf("got %d rolodexes, want 2", len(all))
	}
	// Check both tiers are represented.
	var sawBundled, sawPersonal bool
	for _, r := range all {
		switch r.Visibility {
		case model.VisibilityBundled:
			sawBundled = true
		case model.VisibilityPersonal:
			sawPersonal = true
		}
	}
	if !sawBundled || !sawPersonal {
		t.Fatalf("expected both bundled and personal; sawBundled=%v sawPersonal=%v", sawBundled, sawPersonal)
	}
}

func TestLoadAllEmpty(t *testing.T) {
	tmp := t.TempDir()
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.LoadAll()
	if err != nil {
		t.Fatalf("loadall: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("got %d, want 0", len(all))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL — `LoadAll` undefined.

- [ ] **Step 3: Append implementation to `internal/store/store.go`**

```go
// LoadAll returns every rolodex across every tier (including ephemeral).
// Order: bundled, personal, private, ephemeral. Within a tier, file-order
// from LoadTier. Used by dex search and any other verb that needs full
// graph coverage.
func (s *Store) LoadAll() ([]model.Rolodex, error) {
	var out []model.Rolodex
	for _, v := range []model.Visibility{
		model.VisibilityBundled,
		model.VisibilityPersonal,
		model.VisibilityPrivate,
		model.VisibilityEphemeral,
	} {
		rs, err := s.LoadTier(v)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "$(cat <<'EOF'
Add Store.LoadAll

Returns every rolodex across all four tiers. Used by dex search and any
future verb that needs full graph coverage.
EOF
)"
```

---

## Task 4: `dex search` Verb

Case-insensitive substring match over slug, label, context, and `explore.description`. Walks all rolodexes via `LoadAll`. Output is a JSON-friendly list of matches with parent rolodex context.

**Files:**
- Create: `internal/cli/search.go`
- Create: `internal/cli/search_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/search_test.go`:
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

func writeSearchFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bundled := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"broker-status","label":"Broker status","kind":"info","info":{"content":"x"},"explore":{"description":"Show broker liveness."}},
			{"id":"01HB00000000000000000000E2","slug":"docs","label":"Documentation","kind":"info","info":{"content":"y"}}
		]
	}`
	personal := `{
		"schema_version": 1,
		"id": "01HP00000000000000000000R1",
		"slug": "root",
		"label": "Personal",
		"visibility": "personal",
		"entries": [
			{"id":"01HP00000000000000000000E1","slug":"my-broker-notes","label":"Notes","kind":"info","info":{"content":"z"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(bundled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "personal", "root.json"), []byte(personal), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSubstringMatchesAcrossTiers(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"broker"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got []struct {
		Slug      string `json:"slug"`
		ParentSlug string `json:"parent_slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// broker-status (slug) + my-broker-notes (slug) = 2 hits
	// Plus "Show broker liveness." (explore.description) on broker-status
	// which is the same entry → still one match per entry.
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2: %+v", len(got), got)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"BROKER"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "broker-status") {
		t.Fatalf("expected case-insensitive match; got %q", out.String())
	}
}

func TestSearchNoMatches(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"nonexistent"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("expected []; got %q", out.String())
	}
}

func TestSearchRequiresArg(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("search without arg should error")
	}
}

func TestSearchMatchesExploreDescription(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)
	var out bytes.Buffer
	// "liveness" appears only in broker-status's explore.description.
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"liveness"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "broker-status") {
		t.Fatalf("expected match via explore.description; got %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — `RunSearch`, `SearchOpts` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/search.go`:
```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type SearchOpts struct {
	StoreRoot string
	JSON      bool
	Stdout    io.Writer
	Stderr    io.Writer
}

type searchMatch struct {
	ID         string          `json:"id"`
	Slug       string          `json:"slug"`
	Label      string          `json:"label"`
	Kind       model.EntryKind `json:"kind"`
	ParentID   string          `json:"parent_id"`
	ParentSlug string          `json:"parent_slug"`
	Visibility model.Visibility `json:"visibility"`
}

// RunSearch implements `dex search <query>`. Case-insensitive substring
// match over slug, label, context, and explore.description across all
// rolodexes in every tier.
func RunSearch(opts SearchOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex search: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) != 1 {
		fmt.Fprintln(opts.Stderr, "dex search: requires exactly one query argument")
		return 2
	}
	query := strings.ToLower(argv[0])

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex search: %v\n", err)
		return 1
	}
	rolodexes, err := s.LoadAll()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex search: %v\n", err)
		return 1
	}

	matches := []searchMatch{}
	for _, r := range rolodexes {
		for _, e := range r.Entries {
			if entryMatches(e, query) {
				matches = append(matches, searchMatch{
					ID:         e.ID,
					Slug:       e.Slug,
					Label:      e.Label,
					Kind:       e.Kind,
					ParentID:   r.ID,
					ParentSlug: r.Slug,
					Visibility: r.Visibility,
				})
			}
		}
	}

	if opts.JSON {
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(matches); err != nil {
			fmt.Fprintf(opts.Stderr, "dex search: encode: %v\n", err)
			return 1
		}
		return 0
	}
	if len(matches) == 0 {
		fmt.Fprintln(opts.Stdout, "(no matches)")
		return 0
	}
	for _, m := range matches {
		fmt.Fprintf(opts.Stdout, "%-32s  %s  %s/%s  [%s]\n",
			m.Slug, m.Kind, m.ParentSlug, m.ID, m.Visibility)
	}
	return 0
}

// entryMatches checks whether any of slug, label, context, or
// explore.description (case-folded) contains the lowercase query.
func entryMatches(e model.Entry, lowerQuery string) bool {
	if strings.Contains(strings.ToLower(e.Slug), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Label), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Context), lowerQuery) {
		return true
	}
	if e.Explore != nil && strings.Contains(strings.ToLower(e.Explore.Description), lowerQuery) {
		return true
	}
	return false
}
```

Modify `cmd/dex/main.go` to add `search` case alongside `ls` and `explore`:
```go
	case "search":
		os.Exit(runSearch(os.Args[2:]))
```

And the helper:
```go
func runSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON instead of human output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return cli.RunSearch(cli.SearchOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
		JSON:      *jsonOut,
	}, fs.Args())
}
```

Add to the usage text's `Verbs:` block:
```
  search [--json] <query>
                         Case-insensitive substring search across all
                         entries (slug, label, context, explore desc).
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — all new search tests + everything prior.

- [ ] **Step 5: Smoke test**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-search-smoke
mkdir -p /tmp/dex-search-smoke/{bundled,personal,private,ephemeral}
cat > /tmp/dex-search-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": [
    {"id":"01HB00000000000000000000E1","slug":"hammer","label":"Hammer","kind":"info","info":{"content":"x"}},
    {"id":"01HB00000000000000000000E2","slug":"saw","label":"Saw","kind":"info","info":{"content":"y"}}
  ]
}
JSON
DEX_STORE=/tmp/dex-search-smoke ./dex search hammer
DEX_STORE=/tmp/dex-search-smoke ./dex search HAMMER
DEX_STORE=/tmp/dex-search-smoke ./dex search nothing
DEX_STORE=/tmp/dex-search-smoke ./dex search --json hammer
rm -rf /tmp/dex-search-smoke
```

Expected: `hammer` and `HAMMER` both find `hammer`; `nothing` reports no matches; `--json` returns a JSON array.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/search.go internal/cli/search_test.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex search (case-insensitive substring across all tiers)

Walks every rolodex via Store.LoadAll and matches on slug, label,
context, and explore.description. Per the design's "Substring v1;
revisit if it feels limp" — fuzzy/FTS5 deferred to later.
EOF
)"
```

---

## Task 5: `dex activate` — Pointer + Info Dispatch

First slice of `dex activate`: handle the easy kinds (pointer and info-content). Command-kind support lands in Tasks 6 and 7. Info entries with `provider:` (not `content:`) error explicitly — providers are deferred.

**Files:**
- Create: `internal/cli/activate.go`
- Create: `internal/cli/activate_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/activate_test.go`:
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

func writeActivateFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rootR := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000P1","slug":"tools","label":"Tools","kind":"pointer","pointer":{"to":"01HB00000000000000000000T1"}},
			{"id":"01HB00000000000000000000I1","slug":"readme","label":"Readme","kind":"info","info":{"content":"the readme body"}},
			{"id":"01HB00000000000000000000I2","slug":"dynamic","label":"Dynamic","kind":"info","info":{"provider":"some-provider"}}
		]
	}`
	target := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000T1",
		"slug": "tools-collection",
		"label": "Tools collection",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000H1","slug":"hammer","label":"Hammer","kind":"info","info":{"content":"a hammer"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rootR), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bundled", "tools.json"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActivatePointerDrillsIn(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/tools"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	// JSON form returns the target rolodex's entries (same shape as ls).
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v raw=%s", err, out.String())
	}
	if len(got) != 1 || got[0].Slug != "hammer" {
		t.Fatalf("got %+v", got)
	}
}

func TestActivateInfoContentPrintsContent(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out},
		[]string{"/readme"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "the readme body") {
		t.Fatalf("expected content in stdout; got %q", out.String())
	}
}

func TestActivateInfoProviderUnsupported(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/dynamic"})
	if exit == 0 {
		t.Fatal("info-provider should error in v1")
	}
	if !strings.Contains(errBuf.String(), "provider") {
		t.Fatalf("expected 'provider' in stderr; got %q", errBuf.String())
	}
}

func TestActivateRequiresArg(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("activate without arg should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/activate.go`:
```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/path"
	"github.com/scshafe/dex/internal/store"
)

type ActivateOpts struct {
	StoreRoot string
	JSON      bool
	DryRun    bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunActivate implements `dex activate <ULID|/path> [concern=value]...`.
// Kind-dispatched:
//   - pointer: drills (lists target rolodex's entries; same as `dex ls`)
//   - info with content: prints content
//   - info with provider: errors (v1 — providers deferred)
//   - command: assembles template, validates concerns, execs (Tasks 6/7)
func RunActivate(opts ActivateOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex activate: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) < 1 {
		fmt.Fprintln(opts.Stderr, "dex activate: requires an entry argument (<ULID> or </path>)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex activate: %v\n", err)
		return 1
	}

	entry, _, err := resolveActivateTarget(s, argv[0], opts.Stderr)
	if err != nil {
		return 1
	}

	switch entry.Kind {
	case model.KindPointer:
		return activatePointer(s, entry, opts)
	case model.KindInfo:
		return activateInfo(entry, opts)
	case model.KindCommand:
		// Tasks 6 + 7 fill this in.
		fmt.Fprintln(opts.Stderr, "dex activate: command kind not yet implemented")
		return 2
	default:
		fmt.Fprintf(opts.Stderr, "dex activate: unknown entry kind %q\n", entry.Kind)
		return 1
	}
}

func resolveActivateTarget(s *store.Store, arg string, stderr io.Writer) (model.Entry, model.Rolodex, error) {
	if strings.HasPrefix(arg, "/") {
		root, err := s.MergedRoot()
		if err != nil {
			fmt.Fprintf(stderr, "dex activate: %v\n", err)
			return model.Entry{}, model.Rolodex{}, err
		}
		result, err := path.Resolve(s, root, arg)
		if err != nil {
			fmt.Fprintf(stderr, "dex activate: %v\n", err)
			return model.Entry{}, model.Rolodex{}, err
		}
		return result.Entry, result.ParentRolodex, nil
	}
	e, p, ok, err := s.LookupEntryByID(arg)
	if err != nil {
		fmt.Fprintf(stderr, "dex activate: %v\n", err)
		return model.Entry{}, model.Rolodex{}, err
	}
	if !ok {
		err := fmt.Errorf("entry %q not found", arg)
		fmt.Fprintf(stderr, "dex activate: %v\n", err)
		return model.Entry{}, model.Rolodex{}, err
	}
	return e, p, nil
}

func activatePointer(s *store.Store, entry model.Entry, opts ActivateOpts) int {
	if entry.Pointer == nil {
		fmt.Fprintf(opts.Stderr, "dex activate: pointer entry %q has nil payload\n", entry.Slug)
		return 1
	}
	target, ok, err := s.LookupByID(entry.Pointer.To)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex activate: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex activate: dangling pointer at %q (target %q)\n",
			entry.Slug, entry.Pointer.To)
		return 1
	}
	if opts.JSON {
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(target.Entries); err != nil {
			fmt.Fprintf(opts.Stderr, "dex activate: encode: %v\n", err)
			return 1
		}
		return 0
	}
	for _, e := range target.Entries {
		fmt.Fprintf(opts.Stdout, "%-32s  %s  %s\n", e.Slug, e.Kind, e.Label)
	}
	return 0
}

func activateInfo(entry model.Entry, opts ActivateOpts) int {
	if entry.Info == nil {
		fmt.Fprintf(opts.Stderr, "dex activate: info entry %q has nil payload\n", entry.Slug)
		return 1
	}
	if entry.Info.Provider != "" {
		fmt.Fprintf(opts.Stderr,
			"dex activate: info entry %q uses provider %q; providers are not implemented in v1\n",
			entry.Slug, entry.Info.Provider)
		return 2
	}
	fmt.Fprintln(opts.Stdout, entry.Info.Content)
	return 0
}
```

Modify `cmd/dex/main.go`:
```go
	case "activate":
		os.Exit(runActivate(os.Args[2:]))
```

Helper:
```go
func runActivate(args []string) int {
	fs := flag.NewFlagSet("activate", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON for drillable kinds")
	dryRun := fs.Bool("dry-run", false, "for command kind: print the assembled command without executing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return cli.RunActivate(cli.ActivateOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
		JSON:      *jsonOut,
		DryRun:    *dryRun,
	}, fs.Args())
}
```

Add to usage text:
```
  activate [--json] [--dry-run] <uuid|path> [concern=value]...
                         Run an entry. pointer drills; info prints
                         content; command assembles and execs.
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — 4 new activate tests plus everything prior.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/activate.go internal/cli/activate_test.go cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex activate (pointer + info kinds)

Pointer drills (same as ls); info-content prints; info-provider
errors with "providers not implemented in v1." Command-kind support
lands in the next commits.
EOF
)"
```

---

## Task 6: `dex activate` Command — Template Assembly + `--dry-run`

Now teach `activate` to handle command kind. This task does template assembly and concern validation; actual exec ships in Task 7. `--dry-run` is the gating flag: when set, print the assembled command and exit 0. Without `--dry-run` for command kind, error "exec not yet implemented" — Task 7 fills it in.

**Files:**
- Modify: `internal/cli/activate.go`
- Modify: `internal/cli/activate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/activate_test.go`:
```go
func writeActivateCommandFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000C1",
				"slug": "echo-it",
				"label": "Echo",
				"kind": "command",
				"command": {
					"template": "echo {msg}",
					"concerns": [{
						"id": "01HB00000000000000000000K1",
						"local_id": "msg",
						"slug": "msg-concern",
						"label": "Message",
						"required": true,
						"strict": false
					}]
				}
			},
			{
				"id": "01HB00000000000000000000C2",
				"slug": "echo-default",
				"label": "Echo default",
				"kind": "command",
				"command": {
					"template": "echo {msg}",
					"concerns": [{
						"id": "01HB00000000000000000000K2",
						"local_id": "msg",
						"slug": "msg-concern",
						"label": "Message",
						"default": "hello",
						"required": true,
						"strict": false
					}]
				}
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActivateCommandDryRunSubstitutesConcerns(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out},
		[]string{"/echo-it", "msg=world"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	if !strings.Contains(out.String(), "echo world") {
		t.Fatalf("expected 'echo world' in stdout; got %q", out.String())
	}
}

func TestActivateCommandDryRunUsesDefault(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out},
		[]string{"/echo-default"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "echo hello") {
		t.Fatalf("expected 'echo hello' (from default); got %q", out.String())
	}
}

func TestActivateCommandMissingRequiredConcern(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out, Stderr: &errBuf},
		[]string{"/echo-it"})
	if exit == 0 {
		t.Fatal("missing required concern should error")
	}
	if !strings.Contains(errBuf.String(), "required") {
		t.Fatalf("expected 'required' in stderr; got %q", errBuf.String())
	}
}

func TestActivateCommandWithoutDryRunNotYetImplemented(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/echo-it", "msg=world"})
	if exit == 0 {
		t.Fatal("command without --dry-run should error in this task (exec lands in Task 7)")
	}
}

func TestActivateCommandUnknownConcernIgnored(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out},
		[]string{"/echo-it", "msg=hi", "bogus=ignored"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	// Unknown concerns are accepted silently — they just don't affect
	// substitution. The assembled template still has {msg} → "hi".
	if !strings.Contains(out.String(), "echo hi") {
		t.Fatalf("expected 'echo hi'; got %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — command-kind path still returns "not yet implemented" exit 2.

- [ ] **Step 3: Modify `internal/cli/activate.go`**

Replace the `case model.KindCommand:` arm inside `RunActivate` with:
```go
	case model.KindCommand:
		return activateCommand(entry, argv[1:], opts)
```

Add the new `activateCommand` function below `activateInfo`:
```go
// activateCommand handles `dex activate <command-entry> [concern=value]...`.
// In this task: parse concern args, validate required concerns are
// resolved (via arg or default), substitute the template, and either
// print (--dry-run) or error (exec lands in Task 7).
func activateCommand(entry model.Entry, concernArgs []string, opts ActivateOpts) int {
	if entry.Command == nil {
		fmt.Fprintf(opts.Stderr, "dex activate: command entry %q has nil payload\n", entry.Slug)
		return 1
	}

	// Parse k=v args into a map.
	provided := map[string]string{}
	for _, a := range concernArgs {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			fmt.Fprintf(opts.Stderr, "dex activate: concern arg %q is not of form key=value\n", a)
			return 2
		}
		provided[k] = v
	}

	// Resolve each declared concern: user-provided > default > error if required.
	resolved := map[string]string{}
	for _, c := range entry.Command.Concerns {
		if v, ok := provided[c.LocalID]; ok {
			resolved[c.LocalID] = v
			continue
		}
		if c.Default != "" {
			resolved[c.LocalID] = c.Default
			continue
		}
		if c.Required {
			fmt.Fprintf(opts.Stderr,
				"dex activate: concern %q is required but not provided (and has no default)\n",
				c.LocalID)
			return 1
		}
		resolved[c.LocalID] = ""
	}

	// Substitute {local_id} placeholders.
	assembled := entry.Command.Template
	for k, v := range resolved {
		assembled = strings.ReplaceAll(assembled, "{"+k+"}", v)
	}

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, assembled)
		return 0
	}

	// Real exec lands in Task 7.
	fmt.Fprintln(opts.Stderr, "dex activate: command exec not yet implemented (use --dry-run)")
	return 2
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — five new command-kind tests pass; existing tests still green.

- [ ] **Step 5: Smoke test**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-activate-smoke
mkdir -p /tmp/dex-activate-smoke/{bundled,personal,private,ephemeral}
cat > /tmp/dex-activate-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": [{
    "id": "01HB00000000000000000000C1",
    "slug": "echo-it",
    "label": "Echo",
    "kind": "command",
    "command": {
      "template": "echo hello {who}",
      "concerns": [{
        "id":"01HB00000000000000000000K1","local_id":"who","slug":"who-concern",
        "label":"Who","default":"world","required":true,"strict":false
      }]
    }
  }]
}
JSON
DEX_STORE=/tmp/dex-activate-smoke ./dex activate --dry-run /echo-it
DEX_STORE=/tmp/dex-activate-smoke ./dex activate --dry-run /echo-it who=cole
DEX_STORE=/tmp/dex-activate-smoke ./dex activate /echo-it   # should error: exec not implemented
rm -rf /tmp/dex-activate-smoke
```

Expected: first two print `echo hello world` and `echo hello cole`; third errors.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/activate.go internal/cli/activate_test.go
git commit -m "$(cat <<'EOF'
Implement dex activate command — template assembly + --dry-run

Concerns resolved in order: user-provided > default > error if required.
Unknown concerns are ignored silently (forward-compat: a future schema
revision could add concerns that older agents don't know about).
--dry-run prints the assembled command without executing; running
without --dry-run errors with "not yet implemented" — Task 7 wires
the actual exec.
EOF
)"
```

---

## Task 7: `dex activate` Command — `sh -c` Exec

Final piece: when `dex activate <command-entry>` runs without `--dry-run`, exec the assembled template via `sh -c`. Stdin/stdout/stderr inherit from the calling process; the child's exit code propagates.

**Files:**
- Modify: `internal/cli/activate.go`
- Modify: `internal/cli/activate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/activate_test.go`:
```go
func TestActivateCommandExecSuccess(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [{
			"id":"01HB00000000000000000000C1","slug":"shell-true","label":"true",
			"kind":"command","command":{"template":"true"}
		}]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/shell-true"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}
}

func TestActivateCommandExecPropagatesExitCode(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [{
			"id":"01HB00000000000000000000C1","slug":"shell-false","label":"false",
			"kind":"command","command":{"template":"false"}
		}]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/shell-false"})
	if exit == 0 {
		t.Fatal("expected nonzero exit from `false`")
	}
}

func TestActivateCommandExecStdoutCaptured(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [{
			"id":"01HB00000000000000000000C1","slug":"shell-echo","label":"echo",
			"kind":"command","command":{
				"template":"echo {msg}",
				"concerns":[{
					"id":"01HB00000000000000000000K1","local_id":"msg","slug":"msg-concern",
					"label":"msg","required":true,"strict":false
				}]
			}
		}]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out},
		[]string{"/shell-echo", "msg=hello-from-shell"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "hello-from-shell") {
		t.Fatalf("expected exec stdout in our stdout; got %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL — exec returns exit 2 "not yet implemented."

- [ ] **Step 3: Replace the exec placeholder in `internal/cli/activate.go`**

Add `"os/exec"` to the imports.

In `activateCommand`, replace the trailing block:
```go
	// Real exec lands in Task 7.
	fmt.Fprintln(opts.Stderr, "dex activate: command exec not yet implemented (use --dry-run)")
	return 2
```
with:
```go
	cmd := exec.Command("sh", "-c", assembled)
	cmd.Stdin = os.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	err := cmd.Run()
	if err == nil {
		return 0
	}
	// Propagate the child's exit code if available; otherwise generic 1.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(opts.Stderr, "dex activate: exec failed: %v\n", err)
	return 1
```

You'll also need to add `"errors"` to the imports if it's not already there.

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS — three new exec tests pass plus all prior. The earlier test `TestActivateCommandWithoutDryRunNotYetImplemented` previously asserted a non-zero exit because exec wasn't implemented. That test now needs adjusting since exec works — replace it with a test that's still meaningful, or simply remove it. To keep the file's intent clear, *delete* `TestActivateCommandWithoutDryRunNotYetImplemented` in this step's same edit.

- [ ] **Step 5: Smoke test**

```bash
go build ./cmd/dex
rm -rf /tmp/dex-exec-smoke
mkdir -p /tmp/dex-exec-smoke/{bundled,personal,private,ephemeral}
cat > /tmp/dex-exec-smoke/bundled/root.json <<'JSON'
{
  "schema_version": 1,
  "id": "01HB00000000000000000000R1",
  "slug": "root",
  "label": "Root",
  "visibility": "bundled",
  "entries": [{
    "id": "01HB00000000000000000000C1",
    "slug": "greet",
    "label": "Greet",
    "kind": "command",
    "command": {
      "template": "echo hello, {who}",
      "concerns": [{
        "id":"01HB00000000000000000000K1","local_id":"who","slug":"who-concern",
        "label":"Who","default":"world","required":true,"strict":false
      }]
    }
  }]
}
JSON
DEX_STORE=/tmp/dex-exec-smoke ./dex activate /greet
DEX_STORE=/tmp/dex-exec-smoke ./dex activate /greet who=cole
DEX_STORE=/tmp/dex-exec-smoke ./dex activate --dry-run /greet who=cole
rm -rf /tmp/dex-exec-smoke
```

Expected: first prints `hello, world`; second prints `hello, cole`; third prints the assembled command (`echo hello, cole`) without executing.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/activate.go internal/cli/activate_test.go
git commit -m "$(cat <<'EOF'
Wire dex activate command exec via sh -c

Stdin/stdout/stderr inherit; child's exit code propagates as the
process exit. The assembled template is treated as a shell command —
trust model is "user wrote the template, schema validated it." For
agent-driven invocations the --dry-run flag returns the assembled
command for inspection without executing.
EOF
)"
```

---

## Self-Review

**Spec coverage** (against the design's CLI section and the architect's Q1/Q3 responses):
- `dex explore <entry> [--explore]` from the design — implemented as `dex explore <ULID|/path> [--json]`. The design's `--explore` flag is actually a redundancy (explore IS the explore verb); v1 uses `--json` instead for the structured form. ✓
- `dex search <query>` — substring v1 ✓
- `dex activate <entry> [concern=value]...` — kind-dispatched ✓
- "verb-symmetric `activate` with `exec` as command-specific alias" — `exec` alias *not* added in this slice (`dex activate` works for all kinds; `exec` is a follow-up nice-to-have)
- Q3 path resolution reused for all three verbs ✓
- Q3 ULID-only IDs respected (no path values in stored fields) ✓
- Landmine #1 (provider security): `dex activate` on `info.provider` errors explicitly rather than executing anything ✓

**Out of scope (explicit):**
- `info.provider` execution
- `validator` enforcement on concerns
- `strict: true` enforcement
- `depends_on` resolution
- Path reconstruction in search output
- `exec` as a verb alias for `activate` on commands

**Placeholder scan:** none. Every step has working code or an explicit command. One step in Task 7 deletes a test from Task 6 (`TestActivateCommandWithoutDryRunNotYetImplemented`) — this is an explicit, intentional change documented in that step's instructions.

**Type consistency:**
- `RunExplore` / `ExploreOpts`, `RunSearch` / `SearchOpts`, `RunActivate` / `ActivateOpts` — same shape as `RunLs` / `LsOpts`: injectable `Stdout io.Writer`, `Stderr io.Writer`, `JSON bool`, `StoreRoot string`. `ActivateOpts` additionally carries `DryRun bool`. Consistent.
- `resolveActivateTarget` in Task 5 returns `(model.Entry, model.Rolodex, error)` — the rolodex return is captured by the caller into `_` since none of the kind handlers in Task 5 use it; Task 6's `activateCommand` doesn't need the rolodex either; Task 7's exec doesn't need it. The unused return is a small wart we accept — keeping it leaves the helper reusable if a future kind needs the parent.
- Verb dispatch in `cmd/dex/main.go` follows the same `runVerb(args []string) int` helper pattern for all four verbs.

**One thing to watch during execution:**
- The `errors` import in `internal/cli/activate.go` is added in Task 7 specifically for `errors.As`. If a reviewer thinks the file already imports `errors` (it doesn't, as of Task 5), adjust the import block in Task 7 to add it.
