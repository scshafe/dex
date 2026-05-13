package cli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oklog/ulid/v2"
	"github.com/scshafe/dex/internal/session"
	"github.com/scshafe/dex/internal/store"
)

// Compile-time guard: *store.Store satisfies session.Resolver. The
// reducer was designed against this interface but the assertion lives
// here because this is the first file where both packages are imported
// together.
var _ session.Resolver = (*store.Store)(nil)

// SessionOpts is the shared option set for every session sub-verb.
// SessionDir defaults to ~/.cache/dex/sessions when empty.
type SessionOpts struct {
	StoreRoot  string
	SessionDir string
	Stdout     io.Writer
	Stderr     io.Writer
	Stdin      io.Reader
}

func (o *SessionOpts) normalize() error {
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.SessionDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve session dir: %w", err)
		}
		o.SessionDir = filepath.Join(home, ".cache", "dex", "sessions")
	}
	return nil
}

// manager builds a *session.Manager rooted at opts.SessionDir.
func (o *SessionOpts) manager() *session.Manager {
	return session.NewManager(o.SessionDir, ulid.Monotonic(rand.Reader, 0))
}

// openStore is shared by step / state and any other verb that needs
// the resolver. start / end / list don't need it; for those, pass an
// empty StoreRoot and skip the call.
func (o *SessionOpts) openStore() (*store.Store, error) {
	if o.StoreRoot == "" {
		return nil, errors.New("DEX_STORE not set")
	}
	return store.Open(o.StoreRoot)
}

// RunSessionStep implements `dex session step <id>`. Reads one JSON
// action from stdin, applies it via session.Apply, persists the new
// state, and writes the envelope JSON to stdout.
//
// Exit codes:
//
//	0 — success, or a validation failure encoded as ok=false in the
//	    envelope.
//	1 — protocol/runtime error: missing/unparseable input, missing
//	    session file, Apply returned a Go error (unknown action,
//	    lookup IO failure), or save failed.
//	2 — usage error (missing session id).
func RunSessionStep(opts SessionOpts, args []string) int {
	if err := opts.normalize(); err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: %v\n", err)
		return 1
	}
	if len(args) < 1 {
		fmt.Fprintln(opts.Stderr, "dex session step: requires a session id argument")
		return 2
	}
	id := args[0]

	mgr := opts.manager()
	st, err := mgr.Load(id)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: %v\n", err)
		return 1
	}

	body, err := io.ReadAll(opts.Stdin)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: read stdin: %v\n", err)
		return 1
	}
	if len(body) == 0 {
		fmt.Fprintln(opts.Stderr, "dex session step: empty stdin (expected one JSON action)")
		return 1
	}
	var action session.Action
	if err := json.Unmarshal(body, &action); err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: parse action: %v\n", err)
		return 1
	}

	s, err := opts.openStore()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: %v\n", err)
		return 1
	}

	next, env, err := session.Apply(s, st, action)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: %v\n", err)
		return 1
	}
	if err := mgr.Save(next); err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: save: %v\n", err)
		return 1
	}

	enc := json.NewEncoder(opts.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		fmt.Fprintf(opts.Stderr, "dex session step: encode: %v\n", err)
		return 1
	}
	return 0
}

// RunSessionStart implements `dex session start`. Creates a fresh
// session file and prints {"session_id": "ses_..."}.
func RunSessionStart(opts SessionOpts) int {
	if err := opts.normalize(); err != nil {
		fmt.Fprintf(opts.Stderr, "dex session start: %v\n", err)
		return 1
	}
	mgr := opts.manager()
	st, err := mgr.NewSession()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex session start: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(opts.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]string{"session_id": st.ID}); err != nil {
		fmt.Fprintf(opts.Stderr, "dex session start: encode: %v\n", err)
		return 1
	}
	return 0
}
