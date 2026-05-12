# dex — Design

Status: v2 design (post-architect review). P-11.2 stateless CLI
shipped; session API (P-11.4) is next.

## Origin note

This document was originally authored as "Phase 11" of an epic in
the author's `wm` (window-manager) dotfiles repo, where the
hypertext-launcher idea was first explored as a wm sub-feature. As
the design settled, it became clear the tool is a standalone
primitive — wm is its first activator but not its owner — and it
was relocated here.

The data-structure noun ("a rolodex of entries") is used
throughout this document for now. The project, the binary, and the
CLI are all named `dex`. Whether the data-structure noun should
also rename to "dex" (so a user has many *dexes* that point at each
other) is an open terminology decision; the data model itself is
unaffected either way.

CLI invocations in this document are written as `dex <verb>`,
reflecting the standalone tool. Earlier wm-integrated phrasing
(originally `wm rolodex <verb>` when dex was a wm sub-command) has
been replaced throughout.

## Goal

Build the **rolodex**: a personal, graph-structured, CLI-first workflow
+ knowledge system that ships with two equally-important frontends —
an agent-usable command-line tool and an input-capturing ephemeral
modal overlay bound to `Hyper+P`.

The rolodex is a primitive, not a feature. WM is its first activator
but not its owner. Anything that can spawn a process can use the
rolodex: a key chord, a hotkey daemon, a Stream Deck button, a
Raycast extension, an LLM agent, a shell pipeline.

Its purpose: let the user (and agents acting on the user's behalf)
**save functional CLI-style building blocks** into a navigable graph,
where each node carries its own self-documentation (`--explore`),
each command-leaf carries declared "concerns" pointing at other
rolodexes for value suggestions, and the whole thing composes into
iterative command-building by traversal.

This isn't a smarter Alfred. It's a hypertext personal computing
primitive — Wikipedia hyperlinks meet TLDR pages meet vim-like
modal navigation meet shell composability — designed agent-first
and human-second.

## User Experience

### Two frontends, one system

The user (or an agent) interacts with the rolodex through one of:

**CLI** — the system of record.
```sh
dex ls /commands/broker
dex explore broker.status
dex activate broker.status provider=wm-clock
dex search "pod"
dex add --parent /custom --label "..." --command "..."
dex session start
dex session step <id> "broker.status"
```
Every operation returns structured output (JSON via `--json`,
human-readable by default). Agents are first-class consumers; the
shape of the CLI prioritises their use as much as the user's.

**Modal UI** — `Hyper+P` opens a centered, input-capturing overlay.
Vim-style hjkl + arrows for cursor; single-letter mnemonics for
direct activation; filter-as-you-type when no mnemonic matches.
The modal is a **thin client over the CLI session API**, not a
parallel implementation. Everything it can do, the CLI can do; the
modal just compresses it into keystrokes for human speed.

### The iterative-command-building flow

The killer interaction is iterative composition. Given a command-kind
entry with concerns, the user (or agent) can:

1. `dex explore broker.status` → prints the template, the
   declared concerns, their suggestion-rolodex pointers, defaults,
   and prose narrative + examples.
2. Inspect / pick value for each concern. Concerns whose
   suggestion-rolodex is non-null can be drilled into:
   `dex ls <suggestion-rolodex-uuid>`. Concerns whose source
   is `null` accept free text.
3. Assemble the final invocation:
   `dex activate broker.status provider=wm-clock`.
   Up-arrow + edit + run is the human path; agents construct the
   final string from `--explore` JSON.

In the modal, this same flow is keystrokes: activate the entry,
mnemonic-pick through the concerns' rolodexes, see the assembled
command in a confirmation strip, Enter to run.

### `--explore` as the universal documentation affordance

`--explore` is a first-class flag on every CLI verb that addresses
an entry. It returns the entry's structured self-description:
`description`, `examples: [{description, invocation}]`, `notes`, the
declared concerns (with their suggestion-rolodex pointers), and the
entry's kind + activation contract. Agents use this to learn how
to use any entry; humans use it the same way (with pretty-printing).

## Key Architectural Insights

### 1. CLI-first; modal is a frontend

The rolodex is a CLI tool with a stateful session API. The Swift
modal is a frontend over that API. The CLI is the contract; the
modal is one (privileged, well-integrated) consumer.

Why this matters: agents become first-class users without retrofit.
The same flow that drives the modal (open → navigate → resolve →
activate) drives an agent doing multi-turn composition. There's no
"agent mode" vs "human mode" — there's one system with two clients.

### 2. Input takeover unlocks a free keyboard per level

When the modal claims input, every level of the tree has the full
keyboard available for mnemonics. `t` can mean "terminal" at root,
"tail" inside logs, "test" inside `wm test` — different at every
depth, all unambiguous because context determines meaning. Same
trick that makes Spacemacs which-key and Hammerspoon modals
powerful, but applied here through a tree backed by a CLI session.

### 3. Concerns over parameters

Every command-kind entry has `concerns`, not "parameters." A concern
is **something to figure out before this command makes sense** — it
might be a CLI argument, or it might just be "remind me what
namespace I'm working in." Each concern can point at another
rolodex as a suggestion source. Concerns are explicit graph edges,
the rolodex is a hypergraph, and value-picking is recursive
rolodex navigation.

### 4. Session state, not stateless calls

The session API holds resolved-concern values across a workflow:
once `ns=prod` is picked, every subsequent concern named `ns` in the
same session pre-fills with `prod`. The user (or agent) opens a
session, takes steps, queries state, ends it. The modal opens at
session-start and ends at modal-close. Agents can hold sessions
across multi-turn conversation.

### 5. Visibility, not location

Rolodexes carry a `visibility` field: `bundled | personal | private |
ephemeral`. Storage location is derived from visibility. Agent
writes default to `ephemeral` and require an explicit promotion
before they live alongside the user's curated content.

## Data Model

### Rolodex

```jsonc
{
  "schema_version": 1,
  "id":         "01HQ7AB...",                  // uuid, stable
  "slug":       "broker-providers",            // human handle, locally unique
  "label":      "Broker providers",
  "context":    "All registered wm-broker channels",
  "visibility": "bundled",                     // see Visibility model
  "entries":    [ /* Entry, see below */ ]
}
```

### Entry

```jsonc
{
  "schema_version": 1,
  "id":      "01HQ7CD...",
  "slug":    "broker.status",
  "label":   "Broker status",
  "context": "Snapshot of provider freshness + refcount",

  "kind":    "command",                        // "pointer" | "command" | "info"

  // kind=pointer
  "pointer": { "to": "01HQ..." },

  // kind=command
  "command": {
    "template": "wm broker status --provider {provider}",
    "concerns": [ /* Concern, see below */ ]
  },

  // kind=info
  "info": {
    "content":  "string literal",              // OR
    "provider": "broker-providers-list"        // script-id; output is the content
  },

  // Common: structured self-documentation
  "explore": {
    "description": "Print per-provider refcount, freshness, socket info.",
    "examples": [
      { "description": "All providers", "invocation": "wm broker status" },
      { "description": "Single provider",
        "invocation": "wm broker status --provider wm-clock" }
    ],
    "notes": "Returns non-zero if any provider is stale."
  }
}
```

### Concern

```jsonc
{
  "id":         "provider",
  "label":      "Which provider?",
  "context":    "wm-broker channel id",
  "rolodex":    { "to": "01HQ..." } | null,    // null = pure free-text
  "default":    "wm-clock" | null,
  "required":   false,
  "strict":     false,                         // when true, reject non-rolodex values
  "validator":  "valid-broker-channel" | null, // optional script-id
  "depends_on": []                             // ids of prior concerns whose values
                                               // feed this one's suggestion provider
}
```

**Free-text vs strict** (resolves Q1):
- `rolodex: null` → no suggestions; user types directly.
- `rolodex: <uuid>`, `strict: false` → suggestions offered; user
  may pick or type their own (Tab in modal escapes to typing).
- `rolodex: <uuid>`, `strict: true` → user must pick from the
  rolodex; free input rejected.

**Dependencies** (resolves Q2):
- `depends_on: [ns]` tells the rolodex provider for this concern
  that it will receive `{"ns": "<value>"}` in its context input.
- Linear chains supported in v1; concern resolution order is
  topologically sorted, not declaration-order.

### Visibility model (resolves Q4)

| visibility   | meaning                                                | storage location                                   |
|--------------|--------------------------------------------------------|----------------------------------------------------|
| `bundled`    | ships with dotfiles, read-mostly, public               | `wm/lib/rolodex/store/`                            |
| `personal`   | synced across the user's machines, not public          | `~/.local/share/wm/rolodex/personal/`              |
| `private`    | never leaves this machine                              | `~/.local/share/wm/rolodex/private/`               |
| `ephemeral`  | agent-scratch / inbox; auto-expires (TTL configurable) | `~/.cache/wm/rolodex/`                             |

**Promotion**: `dex promote <rolodex|entry> --to personal`
moves a rolodex (or graduates an entry into a personal rolodex)
between visibility tiers. UUIDs survive promotion → backlinks
survive.

**Agent contribution policy**: `dex add` from inside a
session defaults `visibility: ephemeral`. User explicitly promotes
after review.

### Storage layout (resolves Q3)

- **JSON-per-rolodex**: one file = one rolodex. Filename
  `<slug>.<short-uuid-suffix>.json` (slug for readability + uuid
  suffix to avoid slug collisions across visibilities).
- **Flat directory per visibility**: no nested dirs matching the
  pointer graph. Pointers are by uuid; the directory is a flat
  bag. Logical structure ≠ physical structure.
- **SQLite FTS5 index** is a future, rebuildable-from-JSON cache,
  not parallel storage. Added when `dex search` actually
  feels slow. Not in v1.

## CLI

### Stateless verbs (operate on the store directly)

```sh
dex ls   [<path|uuid>]            # list entries under path/uuid; --json
dex explore <entry>               # structured self-description; --json
dex search <query>                # substring (v1) or FTS5 (later)
dex add    --parent <uuid> ...    # mutate the store; agent writes → ephemeral
dex edit   <uuid> --field=value   # mutate metadata
dex rm     <uuid>                 # tombstone (uuid retained for backlink-safety)
dex promote <uuid> --to personal  # change visibility
dex tree   [<root>]               # render the graph (debug / introspection)
dex doctor                        # validate schema, detect dangling pointers
```

### Activation — verb-symmetric across kinds

```sh
dex activate <entry> [concern=value]...
```

`activate` does the right thing per entry kind:
- `pointer` → returns the target rolodex's metadata + entries
  (CLI equivalent of "drill in"; agent uses to continue navigation)
- `info` → prints / returns content
- `command` → runs the assembled template, after validating all
  required concerns are resolved

`exec` exists as a `command`-specific alias (and errors loudly on
non-command entries) for human ergonomics. Agents should use
`activate` for kind-agnostic dispatch.

### Session API — stateful multi-turn flows

The unlock for the agent-CLI experience and the basis of the modal
implementation.

```sh
dex session start                 # → {"session_id": "ses_..."}
dex session step <id> <input>     # advance the session by one step
dex session state <id>            # current path, resolved concerns, etc.
dex session end <id>              # close session
dex session list                  # debug / cleanup
```

A "step" is a structured input: navigate to a node, drill in,
resolve a concern with a value, pop a level, activate the current
entry. Inputs are typed (`{"action": "drill", "target": "<uuid>"}`,
`{"action": "resolve", "concern": "ns", "value": "prod"}`,
`{"action": "activate"}`). Outputs are the resulting session state.

Resolved-concern cache is per-session. `ns=prod` resolved once
auto-fills any subsequent concern named `ns` in the same session.

The Swift modal opens a session at `Hyper+P` press, drives it via
`session step` calls, closes it on Esc-from-root or activation.
Agents do the same multi-turn from their own context.

### `--explore` flag

`--explore` is supported on `activate`, `ls`, `search` results, and
the entry directly. Returns the structured `explore` object plus the
declared concerns. Agents read this to learn what's possible at a
node.

```sh
dex activate broker.status --explore --json
# returns the entry's structured self-description + concerns dep graph
```

## Modal UI

### Architecture: thin client over session API

The Swift binary (`dex`) is the modal's runtime. On
`Hyper+P`:

1. Spawn an NSPanel with input takeover (NSPanel + activation
   policy `.accessory`, becomes first responder, intercepts keyDown
   events).
2. Call `dex session start` → get session-id.
3. Render the current session state (= root rolodex initially).
4. On every keystroke, translate to a `session step` input,
   send via subprocess call, render the response.
5. On activation of a `command` whose concerns are resolved,
   send `{"action": "activate"}`, close modal on success.

The modal **owns no rolodex semantics**. Tree structure, concern
resolution, action dispatch — all in the CLI runtime. The modal is
a renderer + input router.

### Navigation

| Key                | Behavior                                                        |
|--------------------|-----------------------------------------------------------------|
| `h` `j` `k` `l`    | Move cursor (grid: 2D; list: vertical)                          |
| Arrow keys         | Same as hjkl                                                    |
| `Enter`            | Activate current cell                                           |
| `<letter>`         | If matches a node's `key`, activate. Otherwise start filtering. |
| `Tab`              | In concern-resolution: escape to free-text entry                |
| `Esc`              | Pop one level; at root, close                                   |
| `Esc Esc`          | Close from any depth (TBD: vs single-Esc-closes; pick at P-11.2)|
| `Ctrl-c` / `Ctrl-g`| Close immediately                                               |
| `Ctrl-r`           | Refresh dynamic content (re-run provider)                       |
| `?`                | Toggle help overlay (mnemonic cheatsheet at current level)      |
| `/`                | Force filter-mode (in case a mnemonic shadows your input)       |

### Rendering

- **Grid** by default at every level (per user decision). Cells
  show icon + label + mnemonic chip + subtitle.
- **List** override per node via `render: "list"`.
- **Pagination** for >16 entries; vim-style `J`/`K` paging keys.
- **Breadcrumb strip** at top: `Rolodex › Commands › Broker`.
- **Concern-resolution strip**: when activating a command with
  concerns, replaces the grid with a strip showing each concern,
  its current value (or placeholder), and a pointer to its
  suggestion rolodex.
- **Confirmation strip** for `danger: "destructive"` entries:
  shows the assembled command, requires explicit Enter.

## Agent Integration

Agents are first-class consumers. They:

1. Discover via `dex tree` or `dex search`.
2. Read structured `--explore` output to learn an entry's contract.
3. Open a session for multi-turn workflows.
4. Resolve concerns by drilling into suggestion rolodexes (same
   `session step` mechanism), reading their entries, picking a
   value.
5. Activate the command.
6. Optionally `dex add` new entries (lands in `ephemeral`
   visibility; user reviews and promotes).

The session API + structured explore + JSON output is the
agent-friendly contract. No retrofit; no special agent mode.

## Open Schema Tension (note for future, don't resolve in v1)

**Entries and concerns may be the same data structure**, viewed
through two lenses. An entry has activation (drill/exec/render); a
concern has resolution (pick/typed/computed). Both have id, slug,
label, context, explore. Concern's "suggestion rolodex" is a
rolodex of entries; picking an entry there yields a value. That
"yield a value" is itself an activation kind.

If we collapse: a single `Node` type with a `produces` field
(`navigation | side-effect | value`) replaces the entry/concern
split. The schema becomes more uniform but loses the helpful
naming distinction at the boundary.

**Decision**: v1 keeps them separate (clearer code, clearer mental
model for the user). Schema versioning is in place; v2 can refactor
without losing data. Track in this doc; don't bake duplication
elsewhere.

## Migration from current state

- `wm/lib/rolodex/root.json` etc. (current P-11.1 commit) are
  restructured into the visibility-aware store. `wm-commands.json`
  becomes a bundled rolodex with `visibility: bundled`. The
  top-level "categories" (root.json) become a *bundled* rolodex
  whose entries are pointers to the per-category rolodexes.
- The existing `wm-cmd` binary (Hyper+P → 18-entry flat palette)
  remains untouched. Rebinding waits until P-11.8.

## Phased Delivery (revised)

| Phase    | Scope                                                                                                                                             | Size  |
|----------|---------------------------------------------------------------------------------------------------------------------------------------------------|-------|
| **P-11.1** | DONE — schema scaffolding + migrated wm-cmd entries (current commit). Files restructured in P-11.2.                                              | done  |
| **P-11.2** | Stateless CLI: `dex ls / explore / activate / search` in Swift or Python or bash (TBD). Reads JSON store; no session API yet.             | med   |
| **P-11.3** | Visibility-aware store layout. Restructure P-11.1's files into bundled/personal/private/ephemeral. Promotion command.                            | small |
| **P-11.4** | Session API: `start / step / state / end / list`. Per-session resolved-concern cache. Linear-chain dependencies between concerns.                | med   |
| **P-11.5** | Modal UI: `dex` Swift binary + `RolodexKit.swift`. Session-API client. Vim+mnemonic nav + filter. List render only (no grid yet).         | med   |
| **P-11.6** | Grid render at every level. Pagination for >16.                                                                                                  | small |
| **P-11.7** | Info-panel inline rendering inside the modal (`action: info` content shown without closing).                                                     | small |
| **P-11.8** | Mutation CLI: `add`, `edit`, `rm`, `promote`. User extension auto-discovery.                                                                     | med   |
| **P-11.9** | Concern dependencies non-linear (DAGs). Validators. Free-text Tab escape.                                                                        | small |
| **P-11.10** | skhd parser provider (auto-import bindings as a `keybindings` rolodex). Decide wm-keymap fate.                                                  | small |
| **P-11.11** | Hyper+P rebind: skhd `wm cmd` → `dex`. Retire or alias wm-cmd.                                                                            | tiny  |
| **P-11.12** | Polish: structured `explore` examples renderer in modal, danger gating, `?` help overlay, Ctrl-r refresh.                                        | small |

Minimum viable shipment that unlocks the agent-CLI workflow: P-11.2 +
P-11.4. The modal lands at P-11.5 and gains polish through P-11.12.

## Scope Boundaries

In scope (v1):
- JSON-per-rolodex storage with visibility tiers
- Stateless and stateful (session) CLI verbs
- Modal UI as thin client over session API
- Input takeover, vim nav, mnemonics, filter
- Grid + list rendering; pagination
- Free-text + rolodex-suggestion concerns with `strict` flag
- Linear concern dependencies
- Per-session resolved-concern cache
- Bundled migration of wm-cmd entries
- Agent contribution surface (ephemeral inbox + promotion)

Out of scope (deferred to future phases or v2):
- SQLite FTS5 index (added lazily when search perf demands)
- Non-linear concern dependency DAGs (P-11.9)
- Embedding-based suggestion engines (phase-11 future)
- Multi-pane modal layouts
- Cross-machine sync of personal rolodexes (orthogonal — user picks
  their own sync mechanism)
- Encrypted private store (orthogonal; can layer FS-encryption)
- Collapsing entries and concerns into a unified Node type (v2
  schema migration; data preserved)

## Locked Decisions (this doc)

- CLI-first; modal is a thin client over the session API.
- Three entry kinds (`pointer | command | info`) in v1; unified
  Node type tracked as v2 candidate.
- Concerns are first-class with `rolodex | null`, `strict`,
  `validator`, `depends_on`.
- Linear concern dependency chains in v1 (topologically sorted).
- Session-scoped resolved-concern cache from v1.
- Visibility-as-axis: `bundled | personal | private | ephemeral`;
  storage location is derived.
- JSON-per-rolodex; flat directories per visibility; uuid-keyed.
- Structured `explore`: `description | examples | notes`.
- Verb-symmetric `activate` with `exec` alias for command kind.
- Agent writes default to `ephemeral`; explicit promotion required.

## Open Decisions (per-phase, deferred)

- **Single-Esc-closes vs Esc-pops + double-Esc-closes** in modal
  (decide P-11.5 by trying both).
- **Grid pagination vs scrolling** at >16 entries (decide P-11.6).
- **Filter-mode scoring**: substring vs fuzzy. Substring v1;
  revisit if it feels limp.
- **Concern-provider runtime contract**: stdin JSON vs `--context`
  flag vs env vars. Lean: stdin JSON for structured input. Decide
  P-11.4.
- **`dex search` indexing**: SQLite FTS5 vs in-memory scan
  vs ripgrep. Decide when we feel the pain.
- **Session lifetime + GC**: how long do orphaned sessions live?
  TTL? Explicit cleanup? Decide P-11.4.
- **wm-keymap fate** (subsume into rolodex/keybindings or keep
  parallel). Decide P-11.10.
- **Implementation language for the stateless CLI**: Swift
  (matches modal), Python (fast iteration), or bash (zero
  dependencies). Decide P-11.2.

## Risk / Unknowns

- **Input takeover under macOS focus model**: NSPanel with
  `.accessory` activation policy + first-responder mgmt should
  catch all keys. If anything leaks to skhd or the underlying app
  we fall back to CGEventTap (more invasive; Accessibility perm
  required). Test in P-11.5.
- **Subprocess overhead in modal session loop**: every keystroke
  fires `dex session step` as a subprocess. If that's >30ms
  it'll feel laggy. Mitigation: keep the stateless CLI ultra-thin;
  if needed, the session daemon can be a persistent process the
  modal talks to via Unix socket (premature; profile first).
- **Concern provider failure**: a buggy provider crashes the
  resolution flow. Mitigation: wrap provider invocations; surface
  failures as a placeholder leaf with a retry affordance.
- **Schema migration**: as the schema evolves, `schema_version` on
  every rolodex + entry + concern lets a migration tool walk the
  store. Bake from day one; never ship anything without it.
- **Mnemonic collisions across visibility merges**: bundled +
  personal + private merged at modal-open might collide on `key`
  at the same parent. Rule (locked): first-loaded wins; later
  warns and gets next-available letter. Bundled load order is
  alphabetical for determinism.
- **Sync of `personal` across machines**: out of scope here, but
  the user's mechanism (private git remote, syncthing, etc.) must
  handle JSON files cleanly. Atomic writes (tempfile + rename)
  prevent partial-state syncs.

## Relationship to Existing Surfaces

### Phase 6 (Command/action/keybinding registry)

When Phase 6 lands, the rolodex's bundled `wm-commands` and
`keybindings` rolodexes switch to reading from the Phase 6 registry
instead of hand-authored JSON. Substitution at the provider
boundary; the rolodex schema doesn't change.

### wm-keymap (Hyper+I, existing)

Current Hyper+I opens the context-aware shortcut overlay. P-11.10
decides whether to:
- (A) Keep wm-keymap as a separate fast-path; both consume Phase 6
  registry once it exists.
- (B) Subsume into rolodex with `Hyper+I` rebound to
  `dex activate /keybindings`.

### wm-cmd (Hyper+P, existing)

Manifest entries already migrated in P-11.1. Binary retired or
aliased at P-11.11.

### wm-picker / wm-search / wm-clipboard / wm-control

Standalone surfaces retained. The rolodex can `spawn` them via
action type so they're reachable from the unified entry. Their
dedicated chords remain.

## Inspirations / Prior Art

- **Spacemacs which-key / Doom Emacs leader keys** — modal
  rebinding insight.
- **vim leader sequences** (`,r`, `,f`) — key-tree semantics.
- **Hammerspoon modals** — macOS overlay precedent.
- **TLDR pages / fish abbreviations** — self-documenting commands,
  but flat.
- **Raycast / Alfred** — fuzzy + extensions, but no input takeover,
  no mode-rebinding, no hypertext.
- **Roam / Logseq / Obsidian** — graph-structured personal data,
  but not CLI-first, not agent-usable, no command execution.
- **Notion databases with backlinks** — structured personal store,
  but visual-first.
- **Jupyter notebooks** — interactive command building, but flat
  and per-notebook.

The rolodex sits at the intersection of all of these and brings:
- Hypertext personal knowledge graph
- + CLI-first, agent-usable
- + Vim-style modal navigation under input takeover
- + Iterative command composition via concerns
- + Visibility-aware storage
- + Session-state across multi-turn workflows

None of the inspirations do all of these together. That's the bet.
