package session

import (
	"fmt"
	"time"

	"github.com/scshafe/dex/internal/model"
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
	}

	return st, Envelope{}, fmt.Errorf("session: unknown action %q", a.Type)
}

func applyDrill(r Resolver, st State, a Action) (State, Envelope, error) {
	// UUID target only in this task. Path target lands in Task 3.
	rdx, ok, err := r.LookupByID(a.Target)
	if err != nil {
		return st, Envelope{}, fmt.Errorf("session: lookup: %w", err)
	}
	if !ok {
		return st, failure(st, ErrInvalidTarget,
			fmt.Sprintf("no rolodex with id %q", a.Target), "", ""), nil
	}

	next := touch(st)
	next.PreviousCursors = append(next.PreviousCursors, st.Cursor)
	next.Cursor = Cursor{
		RolodexID: rdx.ID,
		Path:      "", // UUID target wipes the display path
		Mode:      CursorModeBrowse,
	}
	return next, success(next), nil
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
