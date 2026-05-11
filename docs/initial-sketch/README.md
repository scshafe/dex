# Initial sketch — SUPERSEDED

> **Status: archive only.** This directory contains the original
> v1 manifest scaffolding written during early exploration of the
> dex idea (when it was still called "rolodex" and lived as a
> wm sub-feature). The schema and data layout shown here have
> since been replaced by the v2 design in
> [`../design.md`](../design.md), which incorporates an outside
> architect's review.
>
> Specific things that have changed since these files were written:
> - The data structure is no longer just a static tree of "imports"
>   between manifests; it's a graph of UUID-identified
>   rolodexes/entries.
> - Storage is now visibility-tiered
>   (`bundled | personal | private | ephemeral`) and JSON-per-rolodex,
>   not category-per-import.
> - Entries gained `kind: pointer | command | info`; commands
>   gained `concerns` with optional rolodex pointers, `strict`,
>   and `depends_on`.
> - The platform gained a stateful session API (`session start /
>   step / state / end`) and `--explore` became structured rather
>   than free-form.
>
> These files are kept here for design-history reference. They are
> not loaded by any tooling. Treat the v2 design doc as the source
> of truth.

---

# Rolodex manifests (original v1 documentation, below)

Tree-of-nodes data backing the Hyper+P launcher (Phase 11, see
`wm/plan/@epic-plan-2026-05-08/phase-11.md`). One node per category
or leaf. Tree depth and breadth are unbounded.

The Swift binary `wm-pal` (P-11.2, not yet built) loads `root.json`
at modal-open time, resolves any `import:` references into bundled
sub-manifests, runs any `children_from:` providers, and renders the
tree as a grid (default) or list. The binary lives in the future;
this directory is the data scaffolding that the binary will consume.

## File layout

```
wm/lib/rolodex/
├── README.md            ← this file (schema reference)
├── root.json            ← entry point: top-level grid
├── wm-commands.json     ← imported subtree of wm sub-commands
├── wm-config.json       ← imported subtree of config file edits
├── info.json            ← imported subtree of live information panels
└── custom.json          ← stub for user-extension entries
```

User extensions are merged from `~/.config/wm/rolodex/*.json` at
load time (P-11.6).

## Node schema

Every node is a JSON object with these fields:

```jsonc
{
  // Identity (required for everything)
  "id":       "wm-bag-list",        // stable identifier, unique within parent
  "label":    "Bag: list",          // user-visible name
  "subtitle": "Show active bag",    // optional secondary text
  "icon":     "",                  // optional glyph

  // Navigation (optional but recommended)
  "key":      "l",                  // single-letter mnemonic at this level
  "shortcut": "Hyper+G",            // informational only — display hint
                                    // of the global skhd binding if any

  // Render override (optional)
  "render":   "grid",               // "grid" | "list" — default per level

  // EXACTLY ONE of children / import / children_from / action

  "children": [ { ... }, ... ],     // static array of child nodes

  "import":   "manifest:wm-commands", // load wm-commands.json and inline
                                      // its `children` here (P-11.2)

  "children_from": "provider:doctor", // run providers/doctor.sh which emits
                                      // a JSON array of nodes (P-11.4)

  "action": {                       // leaf — one of these sub-fields:
    "wm":    ["broker", "reload"],         // → wm <args>
    "shell": "open -a Activity Monitor",   // → /bin/sh -c <string>
    "open":  { "app": "WezTerm" },         // → NSWorkspace.openApplication
    "info":  "provider:doctor",            // → render inline info panel (P-11.5)
    "spawn": "wm-keymap open"              // → close modal, spawn picker
  },

  // Optional metadata (P-11.9 / P-11.10)
  "context": {                      // hide node when context doesn't match
    "skhd_mode":   "default",
    "nav_mode":    "intra",
    "context_tag": "display-notch:present",
    "fn":          "provider:availability"
  },
  "danger":   "destructive"         // hides behind confirmation prompt
}
```

### `import:` vs `children_from:` vs `children`

- **`children`** — static array, inlined directly in this file. Best
  for small, stable lists.
- **`import: "manifest:NAME"`** — loads `wm/lib/rolodex/NAME.json` at
  modal-open time and inlines its `children` array. Best for keeping
  per-domain manifests in their own files.
- **`children_from: "provider:NAME"`** — runs
  `wm/rolodex/providers/NAME.sh` at node-entry time; the script
  emits a JSON array of nodes on stdout. Best for live data
  (displays geometry, broker status, doctor output).

Exactly one of these (or `action`) must be present on a node.

### Import file format

A file referenced by `import:` is a JSON object:

```jsonc
{
  "version":  1,
  "id":       "wm-commands-manifest",  // for validation
  "children": [ /* array of node objects */ ]
}
```

`version` and `id` are metadata for the loader; `children` is the
payload that gets inlined into the importing node.

### Action types

- **`wm: [arg, ...]`** — exec'd as `wm <arg> <arg> ...`. Detaches;
  modal closes immediately.
- **`shell: "string"`** — exec'd as `/bin/sh -c "<string>"`. Detaches.
- **`open: { app: "..." }`** — explicit app launch via
  NSWorkspace.openApplication. Detaches.
- **`info: "provider:NAME"`** — renders the provider's stdout inline
  in the modal (read-only). User presses Esc to return to parent.
  Not yet implemented (P-11.5).
- **`spawn: "binary args"`** — closes modal, then spawns the named
  picker (wm-keymap, wm-search, wm-picker, wm-control, etc.). Not
  yet implemented (P-11.5).

### Mnemonic conventions

- One letter per node at each level. Collisions within the same
  level are an error at load time.
- Prefer the first letter of the label. Fall back to a salient
  consonant if collisions force it.
- Lowercase only. Caps are reserved as a future signal (maybe
  "danger" indicator).
- Some keys are reserved by the navigation model and can't be used
  as mnemonics at any level:
  - `h` `j` `k` `l` — vim cursor (could overlap if user types fast;
    TBD — see phase-11 doc open decisions)
  - Actually: vim keys ARE allowed as mnemonics; precedence rule is
    "mnemonic wins over cursor when the letter matches a node's
    `key`". So `l` for "logs" works; user uses arrow keys or J to
    move down.
  - Reserved unconditionally: `Enter`, `Esc`, `Tab`, `?`, `/`.

## Versioning

Top-level files (root.json, imports) carry `"version": 1`. Bump
when the schema changes incompatibly. The loader will refuse to
load a manifest with a version it doesn't understand.

## Validation

Until the Swift binary lands, validate manifests by hand with
`jq`:

```sh
jq -e '.' wm/lib/rolodex/*.json   # syntax check
jq -e '[.children[].key] | unique | length == length' \
   wm/lib/rolodex/wm-commands.json  # mnemonic uniqueness
```
