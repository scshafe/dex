# dex

> A pokédex for your shell. Navigate a personal graph; compose CLI
> workflows by traversing linked nodes. Self-documenting,
> agent-usable, with an input-capturing modal frontend.

**Status: design phase.** No implementation yet. The full
specification lives at [`docs/design.md`](docs/design.md).

## What is this?

`dex` is a personal, graph-structured CLI tool plus an
input-capturing modal launcher for macOS. Each entry in your dex
is one of:

- a **pointer** to another dex (drill into a sub-collection),
- a **command** with declared *concerns* — parameters whose values
  are themselves picked by navigating into other dexes, or
- an **info** node that prints contextual notes.

Every entry self-documents via `--explore`, so you (or an agent
acting on your behalf) can iteratively compose a command by
drilling through the graph and resolving its concerns one at a
time.

## Architecture in one paragraph

`dex` is **CLI-first**. Every operation is a structured subcommand
returning JSON. The modal UI — opened by a global hotkey — is a
thin client over a stateful session API. Agents are first-class
consumers using the same session API for multi-turn command
construction. Storage is JSON-per-dex on disk, organized by a
visibility tier (`bundled | personal | private | ephemeral`).

For the full design, including the data model, CLI surface,
session API, modal navigation, and phased delivery, see
[`docs/design.md`](docs/design.md).

## Why?

Three needs that nothing else on macOS combines:

- **Hypertext personal command index.** Wikipedia-style hyperlinks
  for your terminal.
- **CLI-first, agent-usable.** Every interaction works through
  structured subcommands; humans and LLMs use the same primitives.
- **Input-takeover modal.** Quick keyboard-driven access without
  polluting the global keymap. Every level rebinds the keyboard
  for context-specific mnemonics.

## Repository layout

```
dex/
├── README.md            ← you are here
├── docs/
│   ├── design.md        ← full specification (v2)
│   └── initial-sketch/  ← pre-design-review JSON scaffolding,
│                          retained for reference
└── (implementation lands later)
```

## License

TBD — likely MIT or Apache 2.0.
