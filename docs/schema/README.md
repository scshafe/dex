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

## Sync to embedded copy

The Go binary embeds these files from `internal/schema/schemas/`. After
editing any file in this directory, run:

    cp docs/schema/*.json internal/schema/schemas/

A future task will replace this with a `go generate` hook.
