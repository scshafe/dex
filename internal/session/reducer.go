package session

import (
	"fmt"
	"time"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/path"
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
	case ActionPop:
		return applyPop(st)
	}

	return st, Envelope{}, fmt.Errorf("session: unknown action %q", a.Type)
}

// applyPop replaces the cursor with the most recent PreviousCursors
// entry, decrementing that stack. An empty stack is a no-op (still
// returns ok=true), but Version is bumped because pop is a step.
//
// Reslicing operates on next.PreviousCursors — which touch already
// deep-copied — so we never touch the caller's backing storage.
func applyPop(st State) (State, Envelope, error) {
	next := touch(st)
	if n := len(next.PreviousCursors); n > 0 {
		next.Cursor = next.PreviousCursors[n-1]
		next.PreviousCursors = next.PreviousCursors[:n-1]
	}
	return next, success(next), nil
}

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
		// underlying error message is preserved so callers can show
		// a useful diagnosis.
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

// touch returns a copy of st with Version bumped and LastTouched
// updated to now. Use this on every successful action.
func touch(st State) State {
	out := st
	out.Version = st.Version + 1
	out.LastTouched = time.Now()
	// Detach from the caller's backing storage so the reducer stays
	// pure: every successful action returns a state that doesn't
	// share mutable structure with its input.
	out.PreviousCursors = append([]Cursor(nil), st.PreviousCursors...)
	out.Resolved = make(map[string]string, len(st.Resolved))
	for k, v := range st.Resolved {
		out.Resolved[k] = v
	}
	out.PendingConcerns = append([]PendingConcern(nil), st.PendingConcerns...)
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
