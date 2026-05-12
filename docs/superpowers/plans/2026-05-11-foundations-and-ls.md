# dex Foundations + `dex ls` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Go project, the canonical JSON Schemas for the data model, and the first stateless verb `dex ls` (lists the merged root or a specified rolodex by uuid).

**Architecture:** Go with stdlib `flag` for CLI dispatch (cobra deferred until the verb surface justifies it). JSON Schemas are authored in `docs/schema/` and embedded into the binary via `embed` for runtime validation. Data model uses a shared `NodeCore` substrate via Go struct embedding (mirrored in JSON Schema via `$defs/node_core` + `allOf`) — encoding the architect's Q2 reframe so the v1 → v2 collapse is a re-tagging, not a migration. Store is JSON-per-rolodex, flat per visibility tier, with merged-root precedence `private > personal > bundled`. **Path resolution is out of scope** for this slice — `dex ls <uuid>` only; `dex ls /commands/broker` lands in the follow-up plan.

**Tech Stack:** Go 1.22+, stdlib `flag` + `embed` + `encoding/json` + `testing`, `github.com/santhosh-tekuri/jsonschema/v5` for schema validation, `github.com/oklog/ulid/v2` for ULID generation (the design's `01HQ...` ids are ULIDs).

---

## File Structure

Files this plan creates:

```
dex/
├── go.mod                                                    Task 1
├── go.sum                                                    Tasks 1, 5
├── cmd/dex/main.go                                           Task 1, expanded 10-11
├── internal/
│   ├── model/
│   │   ├── visibility.go                                     Task 2
│   │   ├── visibility_test.go                                Task 2
│   │   ├── node.go            (NodeCore, Explore, Example)   Task 3
│   │   ├── concern.go                                        Task 3
│   │   ├── entry.go           (Entry + kind payloads)        Task 3
│   │   ├── rolodex.go                                        Task 3
│   │   └── model_test.go      (round-trip JSON tests)        Task 3
│   ├── schema/
│   │   ├── schema.go          (embed + compile + Validate)   Task 5
│   │   ├── schema_test.go     (valid + invalid fixtures)     Task 6
│   │   └── testdata/
│   │       ├── valid/*.json                                  Task 6
│   │       └── invalid/*.json                                Task 6
│   ├── store/
│   │   ├── store.go           (Store, Load, MergedRoot)      Tasks 7-9
│   │   ├── store_test.go                                     Tasks 7-9
│   │   └── testdata/                                         Tasks 7-9
│   └── cli/
│       ├── ls.go                                             Task 10
│       └── ls_test.go                                        Tasks 10-11
├── docs/schema/
│   ├── README.md                                             Task 4
│   ├── rolodex.schema.json    (top-level + $defs)            Task 4
│   ├── entry.schema.json                                     Task 4
│   └── concern.schema.json                                   Task 4
└── .gitignore                 (extend for Go artifacts)      Task 1
```

`internal/` keeps packages unimportable from outside — appropriate for v1; promote individual packages to a public path when an external consumer materializes.

Schemas live in `docs/schema/` (authoritative source, version-controlled, human-readable as docs) and are pulled into the binary via `//go:embed` from `internal/schema/schema.go`. This avoids duplication.

---

## Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `cmd/dex/main.go`
- Modify: `.gitignore`

- [ ] **Step 1: Initialize Go module**

Run:
```bash
cd /Users/coleshaffer/Projects/dex
go mod init github.com/scshafe/dex
```

Expected: creates `go.mod` with `module github.com/scshafe/dex` and Go directive.

- [ ] **Step 2: Extend `.gitignore` for Go**

Append to `.gitignore`:
```
# Go build artifacts
/dex
/bin/
*.test
*.out
coverage.html
```

- [ ] **Step 3: Write hello-world entry point**

Create `cmd/dex/main.go`:
```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		// wired up in Task 10
		fmt.Println("ls: not yet implemented")
	case "version":
		fmt.Println("dex 0.0.0-dev")
	default:
		fmt.Fprintf(os.Stderr, "dex: unknown verb %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: dex <verb> [args]

Verbs:
  ls [<uuid>]   List entries (merged root, or a specific rolodex)
  version       Print version`)
}
```

- [ ] **Step 4: Verify build**

Run:
```bash
go build ./...
./dex version
```

Expected: `dex 0.0.0-dev`. (Binary named `dex` produced in repo root by `go build ./cmd/dex`. The `go build ./...` form produces nothing in cwd; run `go build ./cmd/dex` if you want a binary.)

Adjust if needed:
```bash
go build ./cmd/dex && ./dex version
```

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/dex/main.go .gitignore
git commit -m "$(cat <<'EOF'
Scaffold Go module and dex CLI entry point

Empty verb dispatcher; version + ls stubs. ls implementation lands later
in this plan; subsequent verbs (explore, search, activate) in follow-ups.
EOF
)"
```

---

## Task 2: Visibility Type

**Files:**
- Create: `internal/model/visibility.go`
- Create: `internal/model/visibility_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/model/visibility_test.go`:
```go
package model

import (
	"encoding/json"
	"testing"
)

func TestVisibilityRoundTrip(t *testing.T) {
	cases := []Visibility{
		VisibilityBundled, VisibilityPersonal, VisibilityPrivate, VisibilityEphemeral,
	}
	for _, v := range cases {
		t.Run(string(v), func(t *testing.T) {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Visibility
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != v {
				t.Fatalf("round-trip: got %q want %q", got, v)
			}
		})
	}
}

func TestVisibilityValidate(t *testing.T) {
	if err := VisibilityBundled.Validate(); err != nil {
		t.Fatalf("expected bundled to validate: %v", err)
	}
	if err := Visibility("nonsense").Validate(); err == nil {
		t.Fatalf("expected invalid visibility to error")
	}
}

func TestVisibilityPrecedence(t *testing.T) {
	// Q3 collision rule: private > personal > bundled.
	// Ephemeral is not part of the merged root.
	tiers := []Visibility{VisibilityBundled, VisibilityPersonal, VisibilityPrivate}
	for i := 0; i < len(tiers)-1; i++ {
		if tiers[i].Precedence() >= tiers[i+1].Precedence() {
			t.Fatalf("expected ascending precedence at index %d: %v >= %v",
				i, tiers[i], tiers[i+1])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/...`
Expected: FAIL (package does not exist).

- [ ] **Step 3: Write minimal implementation**

Create `internal/model/visibility.go`:
```go
// Package model contains the dex data-model types.
//
// Entry and Concern embed a shared NodeCore so that the v2 schema collapse
// (entries + concerns into a unified Node type) is a re-tagging, not a
// re-shaping. See docs/design.md "Open Schema Tension" and the architect's
// Q2 response in handoff context.
package model

import "fmt"

type Visibility string

const (
	VisibilityBundled   Visibility = "bundled"
	VisibilityPersonal  Visibility = "personal"
	VisibilityPrivate   Visibility = "private"
	VisibilityEphemeral Visibility = "ephemeral"
)

func (v Visibility) Validate() error {
	switch v {
	case VisibilityBundled, VisibilityPersonal, VisibilityPrivate, VisibilityEphemeral:
		return nil
	}
	return fmt.Errorf("invalid visibility %q", string(v))
}

// Precedence returns the collision-resolution order for merged-root assembly.
// Higher wins. Ephemeral is not part of the merged root and returns 0.
func (v Visibility) Precedence() int {
	switch v {
	case VisibilityBundled:
		return 1
	case VisibilityPersonal:
		return 2
	case VisibilityPrivate:
		return 3
	}
	return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/...`
Expected: PASS, all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/model/visibility.go internal/model/visibility_test.go
git commit -m "$(cat <<'EOF'
Add Visibility type with precedence rule

Encodes the architect's Q3 collision precedence (private > personal >
bundled) — inverse of trust order, so user customization shadows
bundled defaults. Ephemeral is explicitly outside the merged root.
EOF
)"
```

---

## Task 3: Data Model Types (NodeCore, Concern, Entry, Rolodex)

**Files:**
- Create: `internal/model/node.go`
- Create: `internal/model/concern.go`
- Create: `internal/model/entry.go`
- Create: `internal/model/rolodex.go`
- Create: `internal/model/model_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/model/model_test.go`:
```go
package model

import (
	"encoding/json"
	"testing"
)

const sampleRolodex = `{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "broker-providers",
  "label": "Broker providers",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "broker-status",
      "label": "Broker status",
      "kind": "command",
      "command": {
        "template": "wm broker status --provider {provider}",
        "concerns": [
          {
            "id": "01HQ7AB000000000000000CON1",
            "local_id": "provider",
            "slug": "provider-concern",
            "label": "Which provider?",
            "rolodex": { "to": "01HQ7AB000000000000000XYZ1" },
            "required": false,
            "strict": false
          }
        ]
      },
      "explore": {
        "description": "Snapshot of provider freshness.",
        "examples": [
          {"description": "All providers", "invocation": "wm broker status"}
        ]
      }
    },
    {
      "id": "01HQ7AB000000000000000ENT2",
      "slug": "tools",
      "label": "Tools",
      "kind": "pointer",
      "pointer": { "to": "01HQ7AB000000000000000XYZ2" }
    },
    {
      "id": "01HQ7AB000000000000000ENT3",
      "slug": "readme",
      "label": "Readme",
      "kind": "info",
      "info": { "content": "Hello." }
    }
  ]
}`

func TestRolodexRoundTrip(t *testing.T) {
	var r Rolodex
	if err := json.Unmarshal([]byte(sampleRolodex), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.SchemaVersion != 1 {
		t.Fatalf("schema_version: got %d want 1", r.SchemaVersion)
	}
	if got := len(r.Entries); got != 3 {
		t.Fatalf("entries: got %d want 3", got)
	}

	cmd := r.Entries[0]
	if cmd.Kind != KindCommand {
		t.Fatalf("entry[0].kind: got %q want command", cmd.Kind)
	}
	if cmd.Command == nil {
		t.Fatal("entry[0].command nil")
	}
	if len(cmd.Command.Concerns) != 1 {
		t.Fatalf("entry[0].command.concerns: got %d want 1", len(cmd.Command.Concerns))
	}
	concern := cmd.Command.Concerns[0]
	if concern.LocalID != "provider" {
		t.Fatalf("concern.local_id: got %q want provider", concern.LocalID)
	}
	if concern.ID != "01HQ7AB000000000000000CON1" {
		t.Fatalf("concern.id: got %q", concern.ID)
	}
	// NodeCore is embedded; promoted fields should be addressable directly.
	if concern.Label != "Which provider?" {
		t.Fatalf("concern.label: got %q", concern.Label)
	}

	ptr := r.Entries[1]
	if ptr.Kind != KindPointer || ptr.Pointer == nil || ptr.Pointer.To == "" {
		t.Fatalf("entry[1] pointer payload missing")
	}

	info := r.Entries[2]
	if info.Kind != KindInfo || info.Info == nil || info.Info.Content != "Hello." {
		t.Fatalf("entry[2] info payload wrong")
	}

	// Re-marshal and re-unmarshal; check it round-trips structurally.
	b, err := json.Marshal(&r)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var r2 Rolodex
	if err := json.Unmarshal(b, &r2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if r2.ID != r.ID || len(r2.Entries) != len(r.Entries) {
		t.Fatalf("round-trip diverged")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/...`
Expected: FAIL (types not defined).

- [ ] **Step 3: Write minimal implementation**

Create `internal/model/node.go`:
```go
package model

// NodeCore is the identity-and-prose substrate shared by Entry and Concern.
// Embedding promotes the JSON fields to the outer object, so both types
// serialize with id/slug/label/context/explore at the top level.
//
// Architect's Q2: this is the only sharing v1 does between Entry and Concern.
// Activation contracts (kind, pointer/command/info, rolodex pointer for
// concerns) stay separate; v2 introduces a `produces` discriminator that
// re-tags without re-shaping.
type NodeCore struct {
	ID      string   `json:"id"`
	Slug    string   `json:"slug"`
	Label   string   `json:"label"`
	Context string   `json:"context,omitempty"`
	Explore *Explore `json:"explore,omitempty"`
}

type Explore struct {
	Description string    `json:"description,omitempty"`
	Examples    []Example `json:"examples,omitempty"`
	Notes       string    `json:"notes,omitempty"`
}

type Example struct {
	Description string `json:"description"`
	Invocation  string `json:"invocation"`
}
```

Create `internal/model/concern.go`:
```go
package model

// Concern is a parameter-with-suggestions on a command entry.
//
// LocalID is the handle used inside the command template (e.g. `{provider}`);
// ID is the global ULID so v2 can promote concerns to first-class linkable
// nodes without data migration.
type Concern struct {
	NodeCore
	LocalID   string      `json:"local_id"`
	Rolodex   *RolodexRef `json:"rolodex,omitempty"`
	Default   string      `json:"default,omitempty"`
	Required  bool        `json:"required"`
	Strict    bool        `json:"strict"`
	Validator string      `json:"validator,omitempty"` // registered script-id
	DependsOn []string    `json:"depends_on,omitempty"` // local_ids of prior concerns
}

type RolodexRef struct {
	To string `json:"to"`
}
```

Create `internal/model/entry.go`:
```go
package model

type EntryKind string

const (
	KindPointer EntryKind = "pointer"
	KindCommand EntryKind = "command"
	KindInfo    EntryKind = "info"
)

type Entry struct {
	NodeCore
	Kind    EntryKind       `json:"kind"`
	Pointer *PointerPayload `json:"pointer,omitempty"`
	Command *CommandPayload `json:"command,omitempty"`
	Info    *InfoPayload    `json:"info,omitempty"`
}

type PointerPayload struct {
	To string `json:"to"` // ULID of target rolodex
}

type CommandPayload struct {
	Template string    `json:"template"`
	Concerns []Concern `json:"concerns,omitempty"`
}

type InfoPayload struct {
	// Exactly one of Content or Provider must be set. Provider is a
	// registered script-id (architect's landmine #1) — not a free-form
	// shell string.
	Content  string `json:"content,omitempty"`
	Provider string `json:"provider,omitempty"`
}
```

Create `internal/model/rolodex.go`:
```go
package model

// Rolodex is the top-level container. schema_version is on the rolodex
// only — never on individual entries or concerns (architect's landmine #3:
// per-file versioning, not per-node, to keep migrations sane).
type Rolodex struct {
	SchemaVersion int        `json:"schema_version"`
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Label         string     `json:"label"`
	Context       string     `json:"context,omitempty"`
	Visibility    Visibility `json:"visibility"`
	Entries       []Entry    `json:"entries"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/...`
Expected: PASS, all tests in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/model/
git commit -m "$(cat <<'EOF'
Add Rolodex/Entry/Concern data model with NodeCore substrate

Q2 shape: NodeCore (id/slug/label/context/explore) is embedded into Entry
and Concern via Go struct embedding (mirrors JSON Schema $defs/allOf).
Concerns get a global ULID plus a local_id template handle so v2 can
promote them to first-class nodes without data migration. schema_version
lives on the rolodex only, never on inner nodes (per-file versioning).
EOF
)"
```

---

## Task 4: JSON Schema Files

**Files:**
- Create: `docs/schema/README.md`
- Create: `docs/schema/rolodex.schema.json`
- Create: `docs/schema/entry.schema.json`
- Create: `docs/schema/concern.schema.json`

These schemas are the *canonical contract*. The Go types in Task 3 must remain in sync with them; Task 6's tests exercise both directions.

- [ ] **Step 1: Write `docs/schema/README.md`**

```markdown
# dex JSON Schemas

The canonical contract for `dex` on-disk data. Three files:

- `rolodex.schema.json` — the top-level container (what one JSON file
  on disk represents). Defines `$defs/node_core`.
- `entry.schema.json` — references `node_core` via `allOf`; discriminated
  by `kind` ∈ `{pointer, command, info}`.
- `concern.schema.json` — references `node_core` via `allOf`; carries
  a `local_id` template handle in addition to the global `id`.

## Design notes

- **`schema_version` lives only on the rolodex.** One version per file;
  inner nodes inherit. Migrations are file-level.
- **ULIDs everywhere.** `id`, `pointer.to`, `rolodex.to`, etc. use the
  Crockford base32 ULID pattern (`^[0-9A-HJKMNP-TV-Z]{26}$`). No mixing
  with UUID4.
- **Slugs are case-sensitive kebab-case ASCII** (`^[a-z0-9][a-z0-9-]*$`).
  Validated at write time. Retrofit-resistant.
- **Providers and validators are registered script ids**, not free shell
  strings (architect's landmine #1). Pattern `^[a-z0-9][a-z0-9-]*$`.
- **Pointer/rolodex `to` fields must be ULIDs**, never paths. Paths are
  a CLI-boundary convenience, not a storage primitive.

These schemas are embedded into the binary via `//go:embed` from
`internal/schema/schema.go`.
```

- [ ] **Step 2: Write `docs/schema/concern.schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://dex.local/schema/concern.json",
  "title": "Concern",
  "type": "object",
  "allOf": [
    { "$ref": "rolodex.schema.json#/$defs/node_core" }
  ],
  "properties": {
    "id":         { "$ref": "rolodex.schema.json#/$defs/ulid" },
    "slug":       { "$ref": "rolodex.schema.json#/$defs/slug" },
    "label":      { "type": "string", "minLength": 1 },
    "context":    { "type": "string" },
    "explore":    { "$ref": "rolodex.schema.json#/$defs/explore" },
    "local_id":   { "$ref": "rolodex.schema.json#/$defs/local_id" },
    "rolodex":    {
      "type": "object",
      "properties": { "to": { "$ref": "rolodex.schema.json#/$defs/ulid" } },
      "required": ["to"],
      "additionalProperties": false
    },
    "default":    { "type": "string" },
    "required":   { "type": "boolean" },
    "strict":     { "type": "boolean" },
    "validator":  { "$ref": "rolodex.schema.json#/$defs/script_id" },
    "depends_on": {
      "type": "array",
      "items": { "$ref": "rolodex.schema.json#/$defs/local_id" }
    }
  },
  "required": ["id", "slug", "label", "local_id"],
  "additionalProperties": false
}
```

- [ ] **Step 3: Write `docs/schema/entry.schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://dex.local/schema/entry.json",
  "title": "Entry",
  "type": "object",
  "allOf": [
    { "$ref": "rolodex.schema.json#/$defs/node_core" }
  ],
  "properties": {
    "id":      { "$ref": "rolodex.schema.json#/$defs/ulid" },
    "slug":    { "$ref": "rolodex.schema.json#/$defs/slug" },
    "label":   { "type": "string", "minLength": 1 },
    "context": { "type": "string" },
    "explore": { "$ref": "rolodex.schema.json#/$defs/explore" },
    "kind":    { "enum": ["pointer", "command", "info"] },
    "pointer": {
      "type": "object",
      "properties": { "to": { "$ref": "rolodex.schema.json#/$defs/ulid" } },
      "required": ["to"],
      "additionalProperties": false
    },
    "command": {
      "type": "object",
      "properties": {
        "template": { "type": "string", "minLength": 1 },
        "concerns": {
          "type": "array",
          "items": { "$ref": "concern.schema.json" }
        }
      },
      "required": ["template"],
      "additionalProperties": false
    },
    "info": {
      "type": "object",
      "properties": {
        "content":  { "type": "string" },
        "provider": { "$ref": "rolodex.schema.json#/$defs/script_id" }
      },
      "oneOf": [
        { "required": ["content"] },
        { "required": ["provider"] }
      ],
      "additionalProperties": false
    }
  },
  "required": ["id", "slug", "label", "kind"],
  "allOf": [
    {
      "if": { "properties": { "kind": { "const": "pointer" } } },
      "then": { "required": ["pointer"] }
    },
    {
      "if": { "properties": { "kind": { "const": "command" } } },
      "then": { "required": ["command"] }
    },
    {
      "if": { "properties": { "kind": { "const": "info" } } },
      "then": { "required": ["info"] }
    }
  ],
  "additionalProperties": false
}
```

(Note: the duplicated `allOf` key is invalid JSON Schema. Use a single
top-level `allOf` and put the `node_core` ref alongside the `if/then`
clauses. Corrected form:)

Replace the file with:
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://dex.local/schema/entry.json",
  "title": "Entry",
  "type": "object",
  "properties": {
    "id":      { "$ref": "rolodex.schema.json#/$defs/ulid" },
    "slug":    { "$ref": "rolodex.schema.json#/$defs/slug" },
    "label":   { "type": "string", "minLength": 1 },
    "context": { "type": "string" },
    "explore": { "$ref": "rolodex.schema.json#/$defs/explore" },
    "kind":    { "enum": ["pointer", "command", "info"] },
    "pointer": {
      "type": "object",
      "properties": { "to": { "$ref": "rolodex.schema.json#/$defs/ulid" } },
      "required": ["to"],
      "additionalProperties": false
    },
    "command": {
      "type": "object",
      "properties": {
        "template": { "type": "string", "minLength": 1 },
        "concerns": {
          "type": "array",
          "items": { "$ref": "concern.schema.json" }
        }
      },
      "required": ["template"],
      "additionalProperties": false
    },
    "info": {
      "type": "object",
      "properties": {
        "content":  { "type": "string" },
        "provider": { "$ref": "rolodex.schema.json#/$defs/script_id" }
      },
      "oneOf": [
        { "required": ["content"] },
        { "required": ["provider"] }
      ],
      "additionalProperties": false
    }
  },
  "required": ["id", "slug", "label", "kind"],
  "allOf": [
    { "$ref": "rolodex.schema.json#/$defs/node_core" },
    {
      "if":   { "properties": { "kind": { "const": "pointer" } } },
      "then": { "required": ["pointer"] }
    },
    {
      "if":   { "properties": { "kind": { "const": "command" } } },
      "then": { "required": ["command"] }
    },
    {
      "if":   { "properties": { "kind": { "const": "info" } } },
      "then": { "required": ["info"] }
    }
  ],
  "additionalProperties": false
}
```

- [ ] **Step 4: Write `docs/schema/rolodex.schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://dex.local/schema/rolodex.json",
  "title": "Rolodex",
  "type": "object",
  "properties": {
    "schema_version": { "const": 1 },
    "id":             { "$ref": "#/$defs/ulid" },
    "slug":           { "$ref": "#/$defs/slug" },
    "label":          { "type": "string", "minLength": 1 },
    "context":        { "type": "string" },
    "visibility":     { "enum": ["bundled", "personal", "private", "ephemeral"] },
    "entries":        {
      "type": "array",
      "items": { "$ref": "entry.schema.json" }
    }
  },
  "required": ["schema_version", "id", "slug", "label", "visibility", "entries"],
  "additionalProperties": false,

  "$defs": {
    "ulid": {
      "type": "string",
      "pattern": "^[0-9A-HJKMNP-TV-Z]{26}$",
      "description": "Crockford base32 ULID (26 chars, no I/L/O/U)."
    },
    "slug": {
      "type": "string",
      "pattern": "^[a-z0-9][a-z0-9-]*$",
      "description": "Lowercase kebab-case ASCII. Case-sensitive."
    },
    "local_id": {
      "type": "string",
      "pattern": "^[a-z][a-z0-9_]*$",
      "description": "Template handle for a concern (snake_case ASCII)."
    },
    "script_id": {
      "type": "string",
      "pattern": "^[a-z0-9][a-z0-9-]*$",
      "description": "Registered provider/validator script-id. Not a shell command."
    },
    "node_core": {
      "type": "object",
      "properties": {
        "id":      { "$ref": "#/$defs/ulid" },
        "slug":    { "$ref": "#/$defs/slug" },
        "label":   { "type": "string", "minLength": 1 },
        "context": { "type": "string" },
        "explore": { "$ref": "#/$defs/explore" }
      },
      "required": ["id", "slug", "label"]
    },
    "explore": {
      "type": "object",
      "properties": {
        "description": { "type": "string" },
        "examples": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "description": { "type": "string" },
              "invocation":  { "type": "string" }
            },
            "required": ["description", "invocation"],
            "additionalProperties": false
          }
        },
        "notes": { "type": "string" }
      },
      "additionalProperties": false
    }
  }
}
```

- [ ] **Step 5: Commit**

```bash
git add docs/schema/
git commit -m "$(cat <<'EOF'
Add canonical JSON Schemas for rolodex/entry/concern

Encodes architect Q2 (shared $defs/node_core, ULID id-space, concerns
have local_id template handles), Q3 (slug regex, ULID pattern), and the
three landmines: schema_version only on rolodex, providers/validators
are registered script-ids (regex pattern, not free strings), pointer.to
fields constrained to ULID format.
EOF
)"
```

---

## Task 5: Schema Embedding + Validator

**Files:**
- Create: `internal/schema/schema.go`

- [ ] **Step 1: Add dependency**

Run:
```bash
go get github.com/santhosh-tekuri/jsonschema/v5
```

- [ ] **Step 2: Write `internal/schema/schema.go`**

```go
// Package schema embeds and compiles the canonical JSON Schemas and
// exposes a Validate function for rolodex JSON bytes.
package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed schemas/*.json
var schemaFS embed.FS

const rolodexSchemaID = "https://dex.local/schema/rolodex.json"

var compiled *jsonschema.Schema

func init() {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020

	err := fs.WalkDir(schemaFS, "schemas", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		b, err := schemaFS.ReadFile(path)
		if err != nil {
			return err
		}
		// Each schema file's $id is what the compiler resolves by.
		// We pass an arbitrary URL here just to register the resource,
		// matching the file's relative name to its $id is done via $id itself.
		name := strings.TrimPrefix(path, "schemas/")
		if err := c.AddResource("https://dex.local/schema/"+name, strings.NewReader(string(b))); err != nil {
			return fmt.Errorf("add %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("schema: walk embedded fs: %w", err))
	}

	s, err := c.Compile(rolodexSchemaID)
	if err != nil {
		panic(fmt.Errorf("schema: compile rolodex: %w", err))
	}
	compiled = s
}

// Validate checks that raw is a JSON object conforming to the rolodex
// schema. It returns a *jsonschema.ValidationError on failure (which has
// rich detail via .DetailedOutput()).
func Validate(parsed any) error {
	if compiled == nil {
		return fmt.Errorf("schema: not initialized")
	}
	return compiled.Validate(parsed)
}
```

- [ ] **Step 3: Wire the embed source**

The `//go:embed schemas/*.json` directive needs the schemas reachable from
the package directory. Create symlinks or copy the canonical schemas under
`internal/schema/schemas/`. We'll copy (Go's embed doesn't follow symlinks
across module boundaries, and copying makes the source-of-truth explicit
in the test fixtures step).

Run:
```bash
mkdir -p internal/schema/schemas
cp docs/schema/*.json internal/schema/schemas/
```

Add a note to `docs/schema/README.md` (append):
```markdown

## Sync to embedded copy

The Go binary embeds these files from `internal/schema/schemas/`. After
editing any file in this directory, run:

    cp docs/schema/*.json internal/schema/schemas/

A future task will replace this with a `go generate` hook.
```

- [ ] **Step 4: Verify build**

Run:
```bash
go build ./...
```
Expected: clean build. (No test here; Task 6 supplies the validation tests.)

- [ ] **Step 5: Commit**

```bash
git add internal/schema/ docs/schema/README.md go.mod go.sum
git commit -m "$(cat <<'EOF'
Embed JSON Schemas and expose Validate()

Schemas are duplicated from docs/schema/ into internal/schema/schemas/
for go:embed reachability; README documents the sync step. A go-generate
hook can automate this later.
EOF
)"
```

---

## Task 6: Schema Validation Tests

**Files:**
- Create: `internal/schema/schema_test.go`
- Create: `internal/schema/testdata/valid/*.json` (multiple)
- Create: `internal/schema/testdata/invalid/*.json` (multiple)

- [ ] **Step 1: Write the failing test**

Create `internal/schema/schema_test.go`:
```go
package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/schema"
)

func TestValidFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata/valid")
	if err != nil {
		t.Fatalf("read valid dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no valid fixtures present")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata/valid", e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var parsed any
			if err := json.Unmarshal(b, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := schema.Validate(parsed); err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestInvalidFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata/invalid")
	if err != nil {
		t.Fatalf("read invalid dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no invalid fixtures present")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata/invalid", e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var parsed any
			if err := json.Unmarshal(b, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := schema.Validate(parsed); err == nil {
				t.Fatalf("expected schema violation, got valid")
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/schema/...`
Expected: FAIL (no fixtures present).

- [ ] **Step 3: Write the valid fixtures**

`internal/schema/testdata/valid/empty-rolodex.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "empty",
  "label": "Empty rolodex",
  "visibility": "bundled",
  "entries": []
}
```

`internal/schema/testdata/valid/all-three-kinds.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCE",
  "slug": "kinds-demo",
  "label": "All three entry kinds",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "open-tools",
      "label": "Tools",
      "kind": "pointer",
      "pointer": { "to": "01HQ7AB000000000000000XYZ2" }
    },
    {
      "id": "01HQ7AB000000000000000ENT2",
      "slug": "broker-status",
      "label": "Broker status",
      "kind": "command",
      "command": {
        "template": "wm broker status --provider {provider}",
        "concerns": [
          {
            "id": "01HQ7AB000000000000000CON1",
            "local_id": "provider",
            "slug": "provider-concern",
            "label": "Which provider?",
            "rolodex": { "to": "01HQ7AB000000000000000XYZ1" },
            "required": false,
            "strict": false
          }
        ]
      }
    },
    {
      "id": "01HQ7AB000000000000000ENT3",
      "slug": "readme",
      "label": "Readme",
      "kind": "info",
      "info": { "content": "Hello." }
    }
  ]
}
```

`internal/schema/testdata/valid/info-via-provider.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCF",
  "slug": "info-provider",
  "label": "Info via provider",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT4",
      "slug": "dynamic-readme",
      "label": "Dynamic readme",
      "kind": "info",
      "info": { "provider": "broker-providers-list" }
    }
  ]
}
```

- [ ] **Step 4: Write the invalid fixtures**

`internal/schema/testdata/invalid/missing-schema-version.json`:
```json
{
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "no-version",
  "label": "Missing schema_version",
  "visibility": "bundled",
  "entries": []
}
```

`internal/schema/testdata/invalid/uppercase-slug.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "BadSlug",
  "label": "Uppercase slug",
  "visibility": "bundled",
  "entries": []
}
```

`internal/schema/testdata/invalid/pointer-to-non-ulid.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "bad-pointer",
  "label": "Pointer to path string",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "elsewhere",
      "label": "Elsewhere",
      "kind": "pointer",
      "pointer": { "to": "/commands/broker" }
    }
  ]
}
```

`internal/schema/testdata/invalid/info-with-shell-string-provider.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "bad-provider",
  "label": "Provider is shell string, not script-id",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "evil",
      "label": "Evil info",
      "kind": "info",
      "info": { "provider": "rm -rf ~" }
    }
  ]
}
```

`internal/schema/testdata/invalid/command-kind-without-payload.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "missing-payload",
  "label": "Command kind without command payload",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "naked",
      "label": "Naked command",
      "kind": "command"
    }
  ]
}
```

`internal/schema/testdata/invalid/schema-version-on-entry.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "versioned-entry",
  "label": "schema_version leaked onto entry",
  "visibility": "bundled",
  "entries": [
    {
      "schema_version": 1,
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "naked",
      "label": "Naked",
      "kind": "info",
      "info": { "content": "hi" }
    }
  ]
}
```

(This last one relies on `additionalProperties: false` in entry.schema.json
to reject the extra field.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/schema/... -v`
Expected: all valid fixtures PASS, all invalid fixtures PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/schema/schema_test.go internal/schema/testdata/
git commit -m "$(cat <<'EOF'
Add schema validation tests with valid + invalid fixtures

Invalid fixtures cover each landmine: schema_version leaking onto an
entry (rejected by additionalProperties:false), pointer.to as a path,
info.provider as a shell string, uppercase slug, command kind without
its payload.
EOF
)"
```

---

## Task 7: Store Layout — Tier Discovery

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/store/store_test.go`:
```go
package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

func TestOpenMissingDir(t *testing.T) {
	tmp := t.TempDir()
	_, err := store.Open(filepath.Join(tmp, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error opening missing dir")
	}
}

func TestOpenEmptyStore(t *testing.T) {
	tmp := t.TempDir()
	for _, tier := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(tmp, tier), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", tier, err)
		}
	}
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tiers := s.Tiers()
	if len(tiers) != 4 {
		t.Fatalf("tiers: got %d want 4", len(tiers))
	}
	for _, v := range []model.Visibility{
		model.VisibilityBundled, model.VisibilityPersonal,
		model.VisibilityPrivate, model.VisibilityEphemeral,
	} {
		if _, ok := tiers[v]; !ok {
			t.Fatalf("missing tier %s", v)
		}
	}
}

func TestOpenMissingTierDir(t *testing.T) {
	// Only bundled present; others auto-created.
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "bundled"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(s.Tiers()) != 4 {
		t.Fatalf("expected 4 tier dirs (auto-created), got %d", len(s.Tiers()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/store.go`:
```go
// Package store reads dex rolodex files from disk, organized by
// visibility tier.
//
// Layout (rooted at the store path):
//
//   <root>/bundled/<slug>.<short>.json
//   <root>/personal/<slug>.<short>.json
//   <root>/private/<slug>.<short>.json
//   <root>/ephemeral/<slug>.<short>.json
//
// Tier directories are created on Open if missing. This makes a fresh
// install zero-friction: `mkdir ~/.local/share/dex && dex ls` works.
package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scshafe/dex/internal/model"
)

type Store struct {
	root  string
	tiers map[model.Visibility]string
}

func Open(root string) (*Store, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("store: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("store: root %q is not a directory", root)
	}

	tiers := map[model.Visibility]string{
		model.VisibilityBundled:   filepath.Join(root, "bundled"),
		model.VisibilityPersonal:  filepath.Join(root, "personal"),
		model.VisibilityPrivate:   filepath.Join(root, "private"),
		model.VisibilityEphemeral: filepath.Join(root, "ephemeral"),
	}
	for v, p := range tiers {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s tier: %w", v, err)
		}
	}
	return &Store{root: root, tiers: tiers}, nil
}

func (s *Store) Root() string { return s.root }

// Tiers returns the tier-directory map. The returned map is a fresh copy;
// callers may not mutate the Store via this method.
func (s *Store) Tiers() map[model.Visibility]string {
	out := make(map[model.Visibility]string, len(s.tiers))
	for k, v := range s.tiers {
		out[k] = v
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "$(cat <<'EOF'
Add Store.Open with tier-directory auto-creation

Tier dirs (bundled/personal/private/ephemeral) are created if missing
so a fresh DEX_STORE works without manual mkdir.
EOF
)"
```

---

## Task 8: Store Loading — Read + Parse Rolodex Files

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Create: `internal/store/testdata/simple/bundled/root.01HQ7AB.json`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:
```go
func TestLoadTier(t *testing.T) {
	s, err := store.Open("testdata/simple")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rolodexes, err := s.LoadTier(model.VisibilityBundled)
	if err != nil {
		t.Fatalf("load bundled: %v", err)
	}
	if len(rolodexes) != 1 {
		t.Fatalf("rolodexes: got %d want 1", len(rolodexes))
	}
	r := rolodexes[0]
	if r.Slug != "root" {
		t.Fatalf("slug: got %q want root", r.Slug)
	}
	if r.Visibility != model.VisibilityBundled {
		t.Fatalf("visibility: got %q want bundled", r.Visibility)
	}
	if len(r.Entries) != 1 {
		t.Fatalf("entries: got %d want 1", len(r.Entries))
	}
}

func TestLoadTierEmpty(t *testing.T) {
	tmp := t.TempDir()
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rolodexes, err := s.LoadTier(model.VisibilityBundled)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(rolodexes) != 0 {
		t.Fatalf("expected 0 rolodexes, got %d", len(rolodexes))
	}
}

func TestLoadTierRejectsInvalid(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "bundled"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"schema_version":1,"slug":"missing-id","label":"x","visibility":"bundled","entries":[]}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.LoadTier(model.VisibilityBundled)
	if err == nil {
		t.Fatal("expected schema-validation error on missing id")
	}
}
```

Create the fixture `internal/store/testdata/simple/bundled/root.01HQ7AB.json`:
```json
{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000R001",
  "slug": "root",
  "label": "Bundled root",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "tools",
      "label": "Tools",
      "kind": "pointer",
      "pointer": { "to": "01HQ7AB000000000000000R002" }
    }
  ]
}
```

Also create the other tier directories so Open() doesn't have to create them inside the testdata tree:
```bash
mkdir -p internal/store/testdata/simple/{personal,private,ephemeral}
touch internal/store/testdata/simple/{personal,private,ephemeral}/.gitkeep
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL (LoadTier undefined).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/store.go`:
```go
import (
	"encoding/json"
)

// LoadTier reads every `*.json` file under the given visibility's tier
// directory, validates each against the embedded schema, and returns the
// parsed Rolodexes. Files with extension other than `.json` are skipped.
// Validation errors are returned as a single wrapped error containing the
// offending file's path.
func (s *Store) LoadTier(v model.Visibility) ([]model.Rolodex, error) {
	dir, ok := s.tiers[v]
	if !ok {
		return nil, fmt.Errorf("store: unknown visibility %q", v)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("store: read tier %s: %w", v, err)
	}

	var out []model.Rolodex
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		r, err := s.readRolodex(path)
		if err != nil {
			return nil, fmt.Errorf("store: %s: %w", path, err)
		}
		if r.Visibility != v {
			return nil, fmt.Errorf("store: %s: visibility %q does not match tier dir %q",
				path, r.Visibility, v)
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) readRolodex(path string) (model.Rolodex, error) {
	var zero model.Rolodex
	b, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	// Validate against schema first (preserves rich error from the validator),
	// then unmarshal into the typed struct.
	var parsed any
	if err := json.Unmarshal(b, &parsed); err != nil {
		return zero, fmt.Errorf("parse: %w", err)
	}
	if err := schemaValidate(parsed); err != nil {
		return zero, fmt.Errorf("schema: %w", err)
	}
	var r model.Rolodex
	if err := json.Unmarshal(b, &r); err != nil {
		return zero, fmt.Errorf("decode: %w", err)
	}
	return r, nil
}
```

The `schemaValidate` reference needs an import. Add to the imports block:
```go
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/schema"
)
```

And replace the call to `schemaValidate(parsed)` with `schema.Validate(parsed)`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/... -v`
Expected: PASS (all four store tests, including new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/store/ 
git commit -m "$(cat <<'EOF'
Add Store.LoadTier with schema validation

LoadTier reads *.json in a tier directory, validates each against the
embedded schema, and returns parsed Rolodexes. Cross-checks each
rolodex's visibility field against the tier dir it was found in
(belt-and-braces against misplaced files).
EOF
)"
```

---

## Task 9: Merged Root with Precedence

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Create: `internal/store/testdata/merge-precedence/...`

The "merged root" is what `dex ls` (no arg) shows: the union of the per-tier
root rolodexes (entries with `slug: "root"`), with collisions resolved by
visibility precedence (private > personal > bundled). Ephemeral is *not*
part of the merged root — it's an agent scratchpad.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:
```go
func TestMergedRootEmpty(t *testing.T) {
	tmp := t.TempDir()
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.MergedRoot()
	if err != nil {
		t.Fatalf("merged root: %v", err)
	}
	if len(root.Entries) != 0 {
		t.Fatalf("expected empty merged root, got %d entries", len(root.Entries))
	}
}

func TestMergedRootPrecedence(t *testing.T) {
	s, err := store.Open("testdata/merge-precedence")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	root, err := s.MergedRoot()
	if err != nil {
		t.Fatalf("merged root: %v", err)
	}
	// The fixture has the slug "tools" defined in both bundled and personal.
	// Personal should win.
	var tools *model.Entry
	for i, e := range root.Entries {
		if e.Slug == "tools" {
			tools = &root.Entries[i]
			break
		}
	}
	if tools == nil {
		t.Fatal("merged root missing slug 'tools'")
	}
	if tools.Label != "Tools (personal)" {
		t.Fatalf("expected personal version to win; got label %q", tools.Label)
	}
}
```

Create fixtures:

`internal/store/testdata/merge-precedence/bundled/root.01HB.json`:
```json
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
      "label": "Tools (bundled)",
      "kind": "pointer",
      "pointer": { "to": "01HB00000000000000000000T1" }
    },
    {
      "id": "01HB00000000000000000000E2",
      "slug": "only-in-bundled",
      "label": "Only here",
      "kind": "pointer",
      "pointer": { "to": "01HB00000000000000000000T2" }
    }
  ]
}
```

`internal/store/testdata/merge-precedence/personal/root.01HP.json`:
```json
{
  "schema_version": 1,
  "id": "01HP00000000000000000000R1",
  "slug": "root",
  "label": "Personal root",
  "visibility": "personal",
  "entries": [
    {
      "id": "01HP00000000000000000000E1",
      "slug": "tools",
      "label": "Tools (personal)",
      "kind": "pointer",
      "pointer": { "to": "01HP00000000000000000000T1" }
    }
  ]
}
```

Also create empty `private/` and `ephemeral/` dirs with `.gitkeep`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL (MergedRoot undefined).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/store.go`:
```go
const RootSlug = "root"

// MergedRoot constructs the merged root rolodex by union-ing the per-tier
// root rolodexes (those with slug == "root") in precedence order
// (private > personal > bundled). Ephemeral is excluded.
//
// Collisions are resolved by entry slug: a higher-precedence entry shadows
// a lower-precedence entry with the same slug. The shadowed entry is
// reachable via `<slug>@<visibility>` addressing in a later iteration of
// the verb surface — for now it's simply hidden.
func (s *Store) MergedRoot() (model.Rolodex, error) {
	tiers := []model.Visibility{
		model.VisibilityBundled,
		model.VisibilityPersonal,
		model.VisibilityPrivate,
	}

	bySlug := map[string]model.Entry{}
	for _, v := range tiers {
		rolodexes, err := s.LoadTier(v)
		if err != nil {
			return model.Rolodex{}, err
		}
		for _, r := range rolodexes {
			if r.Slug != RootSlug {
				continue
			}
			for _, e := range r.Entries {
				// Lower-precedence is loaded first; later iteration
				// overwrites (= shadows).
				bySlug[e.Slug] = e
			}
		}
	}

	out := model.Rolodex{
		SchemaVersion: 1,
		ID:            "", // synthesized; no on-disk identity
		Slug:          "merged-root",
		Label:         "Merged root",
		Visibility:    model.VisibilityBundled, // a label, not authoritative
		Entries:       make([]model.Entry, 0, len(bySlug)),
	}
	for _, e := range bySlug {
		out.Entries = append(out.Entries, e)
	}
	// Sort by slug for stable output. Test depends on this not being
	// alphabetical if your fixture uses specific ordering; here we don't.
	sort.Slice(out.Entries, func(i, j int) bool {
		return out.Entries[i].Slug < out.Entries[j].Slug
	})
	return out, nil
}
```

Add `"sort"` to the imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "$(cat <<'EOF'
Add MergedRoot with private > personal > bundled precedence

Iterates tiers in ascending-precedence order; later overrides earlier.
Ephemeral is excluded — it's the agent scratchpad, not part of the
merged root. Output is sorted by slug for stable rendering.
EOF
)"
```

---

## Task 10: `dex ls` (No Arg → Merged Root, with --json Flag)

**Files:**
- Create: `internal/cli/ls.go`
- Create: `internal/cli/ls_test.go`
- Modify: `cmd/dex/main.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/ls_test.go`:
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

func writeFixture(t *testing.T, root string) {
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
				"id": "01HB00000000000000000000E1",
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

func TestLsMergedRootJSON(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{
		StoreRoot: tmp,
		JSON:      true,
		Stdout:    &out,
		Stderr:    &errBuf,
	}, nil)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	var got []struct {
		Slug  string `json:"slug"`
		Label string `json:"label"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, out.String())
	}
	if len(got) != 1 || got[0].Slug != "tools" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsMergedRootEmpty(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out}, nil)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("expected [], got %q", out.String())
	}
}

func TestLsHumanReadable(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out}, nil)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "tools") {
		t.Fatalf("expected human output to mention 'tools', got: %s", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/...`
Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/ls.go`:
```go
// Package cli implements the dex command verbs. Each verb is a Run<Verb>
// function that takes an Opts struct and an argv tail; the main package
// wires them into the verb dispatch.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type LsOpts struct {
	StoreRoot string
	JSON      bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunLs implements `dex ls [<uuid>]`. With no arg in argv, prints the
// merged root. With a single ULID arg, prints that rolodex's entries.
// Returns the process exit code.
func RunLs(opts LsOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex ls: store root not set (use DEX_STORE)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
		return 1
	}

	var entries []model.Entry
	switch len(argv) {
	case 0:
		root, err := s.MergedRoot()
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
			return 1
		}
		entries = root.Entries
	case 1:
		// uuid lookup; wired in Task 11.
		fmt.Fprintln(opts.Stderr, "dex ls: <uuid> lookup not yet implemented")
		return 2
	default:
		fmt.Fprintln(opts.Stderr, "dex ls: too many arguments")
		return 2
	}

	if opts.JSON {
		return emitJSON(opts.Stdout, opts.Stderr, entries)
	}
	return emitHuman(opts.Stdout, entries)
}

func emitJSON(stdout, stderr io.Writer, entries []model.Entry) int {
	if entries == nil {
		entries = []model.Entry{}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		fmt.Fprintf(stderr, "dex ls: encode: %v\n", err)
		return 1
	}
	return 0
}

func emitHuman(stdout io.Writer, entries []model.Entry) int {
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(empty)")
		return 0
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%-32s  %s  %s\n", e.Slug, e.Kind, e.Label)
	}
	return 0
}
```

Modify `cmd/dex/main.go` — replace the `ls` case:
```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/scshafe/dex/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		os.Exit(runLs(os.Args[2:]))
	case "version":
		fmt.Println("dex 0.0.0-dev")
	default:
		fmt.Fprintf(os.Stderr, "dex: unknown verb %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runLs(args []string) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "emit JSON instead of human-readable output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return cli.RunLs(cli.LsOpts{
		StoreRoot: os.Getenv("DEX_STORE"),
		JSON:      *jsonOut,
	}, fs.Args())
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: dex <verb> [args]

Verbs:
  ls [--json] [<uuid>]   List entries (merged root, or a specific rolodex)
  version                Print version

Environment:
  DEX_STORE              Path to the store root (must contain
                         bundled/personal/private/ephemeral dirs)`)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./...`
Expected: PASS in all packages.

- [ ] **Step 5: Smoke-test the binary**

```bash
go build ./cmd/dex
mkdir -p /tmp/dex-store/{bundled,personal,private,ephemeral}
DEX_STORE=/tmp/dex-store ./dex ls --json
```

Expected: `[]`

```bash
cat > /tmp/dex-store/bundled/root.json <<'JSON'
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
DEX_STORE=/tmp/dex-store ./dex ls
```

Expected (human output):
```
tools                             pointer  Tools
```

```bash
DEX_STORE=/tmp/dex-store ./dex ls --json
```

Expected: a single-element JSON array containing the `tools` entry.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/ cmd/dex/main.go
git commit -m "$(cat <<'EOF'
Implement dex ls (merged root, --json + human output)

DEX_STORE points at the store root containing tier directories. With
no argument, lists the merged root (private > personal > bundled
precedence). --json emits structured output for agents and the future
modal client; default output is human-readable.
EOF
)"
```

---

## Task 11: `dex ls <uuid>` — Direct Rolodex Lookup

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/cli/ls.go`
- Modify: `internal/cli/ls_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:
```go
func TestLookupByID(t *testing.T) {
	s, err := store.Open("testdata/simple")
	if err != nil {
		t.Fatal(err)
	}
	r, ok, err := s.LookupByID("01HQ7AB000000000000000R001")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected to find the bundled root")
	}
	if r.Slug != "root" {
		t.Fatalf("slug: got %q", r.Slug)
	}
}

func TestLookupByIDMissing(t *testing.T) {
	s, err := store.Open("testdata/simple")
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := s.LookupByID("01HQ7AB000000000000000ZZZZ")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if ok {
		t.Fatal("expected not-found")
	}
}
```

Append to `internal/cli/ls_test.go`:
```go
func TestLsByID(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"01HB00000000000000000000R1"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, out.String())
	}
	if len(got) != 1 || got[0].Slug != "tools" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsByIDNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HZ00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected non-zero exit for not-found")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./...`
Expected: FAIL (LookupByID undefined; uuid arm not implemented).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/store.go`:
```go
// LookupByID scans every tier (including ephemeral) and returns the
// rolodex with the given ID. The second return is false when not found.
// Errors are reserved for IO/schema failures.
func (s *Store) LookupByID(id string) (model.Rolodex, bool, error) {
	for _, v := range []model.Visibility{
		model.VisibilityBundled,
		model.VisibilityPersonal,
		model.VisibilityPrivate,
		model.VisibilityEphemeral,
	} {
		rolodexes, err := s.LoadTier(v)
		if err != nil {
			return model.Rolodex{}, false, err
		}
		for _, r := range rolodexes {
			if r.ID == id {
				return r, true, nil
			}
		}
	}
	return model.Rolodex{}, false, nil
}
```

Modify `internal/cli/ls.go` — replace the `case 1:` arm:
```go
case 1:
	r, ok, err := s.LookupByID(argv[0])
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex ls: rolodex %q not found\n", argv[0])
		return 1
	}
	entries = r.Entries
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -v`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/cli/ls.go internal/store/store_test.go internal/cli/ls_test.go
git commit -m "$(cat <<'EOF'
Implement dex ls <uuid> direct rolodex lookup

Scans every tier including ephemeral. Not-found returns exit 1 with
a stderr message; path resolution (dex ls /commands/broker) is a
follow-up plan.
EOF
)"
```

---

## Self-Review

**Spec coverage** (against the architect's Q2/Q3 recommendations and the design doc):
- Q2 `node_core` substrate: Task 3 (Go) + Task 4 (JSON Schema). ✓
- Q2 concerns get global ULID + local_id: Task 3 + Task 4. ✓
- Q2 single uuid space: implicit; no separate schemas for entry-id vs concern-id. ✓
- Q3 slug regex baked in: Task 4 `$defs/slug`. ✓
- Q3 ULID pointer targets only: Task 4 `$defs/ulid` referenced from pointer.to + rolodex.to. ✓
- Q3 collision precedence private > personal > bundled: Task 2 (Visibility.Precedence) + Task 9 (MergedRoot loops in ascending order). ✓
- Q3 root rolodex per tier: Task 9 (RootSlug = "root", per-tier convention). ✓
- Landmine: provider security via registered script-id: Task 4 `$defs/script_id`, Task 6 invalid fixture. ✓
- Landmine: schema_version only on rolodex: Task 4 (rolodex.schema.json has it required; entry/concern do not) + Task 6 invalid fixture for leaked schema_version. ✓
- Landmine: per-rolodex write lock: *not in this plan* — only the read path lands here; the write path (`dex add`, `dex edit`) is a later plan and write locks ship with it.

**Out of scope (explicitly):**
- Path resolution (`dex ls /commands/broker`): follow-up plan
- The other stateless verbs (`explore`, `search`, `activate`): follow-up plan
- Session API (P-11.4): separate plan
- Modal (P-11.5+): separate plans
- Mutation CLI (`add`, `edit`, `rm`, `promote`): P-11.8 plan with write-lock landing alongside

**Placeholder scan**: ran. No "TBD", "implement later", or unfilled code blocks.

**Type consistency**: Checked. `LsOpts` is the same shape in test and impl. `MergedRoot` returns `model.Rolodex`. `LookupByID` returns `(model.Rolodex, bool, error)`. `Visibility` precedence values are stable across the plan. `RootSlug` is the single source of truth for the "root" string literal.

**Two things to watch during execution:**
1. The `jsonschema/v5` library version may have small API changes. If `c.Draft = jsonschema.Draft2020` doesn't compile, check the package docs — it may be `jsonschema.Draft2020`, `jsonschema.Draft202012`, or a constant under a different name. Adjust per the installed version.
2. The "auto-create tier dirs on Open" behavior (Task 7) makes `testdata/` fixtures slightly awkward — if Go's test runner doesn't have write permission, the tests will fail at Open. Should always work in practice but worth noting if a sandboxed CI fails strangely.
