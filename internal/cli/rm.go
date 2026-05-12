package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/store"
)

type RmOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunRm implements `dex rm <entry-ULID>`. Splices the entry out of its
// parent rolodex and writes the result. Pointers in other rolodexes
// that target the removed entry become dangling — `dex doctor` will
// surface those.
func RunRm(opts RmOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex rm: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) != 1 {
		fmt.Fprintln(opts.Stderr, "dex rm: requires exactly one entry ULID argument")
		return 2
	}
	entryID := argv[0]

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex rm: %v\n", err)
		return 1
	}
	_, parent, ok, err := s.LookupEntryByID(entryID)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex rm: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex rm: entry %q not found\n", entryID)
		return 1
	}

	filtered := parent.Entries[:0]
	for _, e := range parent.Entries {
		if e.ID != entryID {
			filtered = append(filtered, e)
		}
	}
	parent.Entries = filtered

	if err := s.WriteRolodex(parent); err != nil {
		fmt.Fprintf(opts.Stderr, "dex rm: %v\n", err)
		return 1
	}
	return 0
}
