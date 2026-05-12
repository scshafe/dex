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
