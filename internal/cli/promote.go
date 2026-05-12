package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type PromoteOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunPromote implements `dex promote <rolodex-ULID> --to <tier>`. Moves
// the rolodex's file between tier directories and rewrites its
// `visibility` field. The ULID is preserved, so backlinks survive.
func RunPromote(opts PromoteOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex promote: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) < 1 {
		fmt.Fprintln(opts.Stderr, "dex promote: first argument must be the rolodex ULID")
		return 2
	}
	rolodexID := argv[0]

	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	to := fs.String("to", "", "target visibility (bundled|personal|private|ephemeral)")
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}
	if *to == "" {
		fmt.Fprintln(opts.Stderr, "dex promote: --to is required")
		return 2
	}
	target := model.Visibility(*to)
	if err := target.Validate(); err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: %v\n", err)
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: %v\n", err)
		return 1
	}
	r, ok, err := s.LookupByID(rolodexID)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex promote: rolodex %q not found\n", rolodexID)
		return 1
	}
	if r.Visibility == target {
		fmt.Fprintf(opts.Stderr, "dex promote: rolodex %q is already in tier %s\n", rolodexID, target)
		return 0
	}

	origin := r.Visibility
	r.Visibility = target

	// Write to new tier first, then delete from old. Order matters: if
	// the write fails, the original file remains untouched.
	if err := s.WriteRolodex(r); err != nil {
		fmt.Fprintf(opts.Stderr, "dex promote: write to %s failed: %v\n", target, err)
		return 1
	}
	if err := s.DeleteRolodexFile(origin, rolodexID); err != nil {
		fmt.Fprintf(opts.Stderr,
			"dex promote: WARNING: wrote to %s but failed to delete from %s: %v (manual cleanup needed)\n",
			target, origin, err)
		return 1
	}
	return 0
}
