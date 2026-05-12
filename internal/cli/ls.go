// Package cli implements the dex command verbs. Each verb is a Run<Verb>
// function that takes an Opts struct and an argv tail; the main package
// wires them into the verb dispatch.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type LsOpts struct {
	StoreRoot string
	JSON      bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunLs implements `dex ls [<uuid>]`. With no arg in argv, prints the
// merged root. With a single ULID arg, prints that rolodex's entries.
// Returns the process exit code.
func RunLs(opts LsOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex ls: store root not set (use DEX_STORE)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
		return 1
	}

	var entries []model.Entry
	switch len(argv) {
	case 0:
		root, err := s.MergedRoot()
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
			return 1
		}
		entries = root.Entries
	case 1:
		r, ok, err := s.LookupByID(argv[0])
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(opts.Stderr, "dex ls: rolodex %q not found\n", argv[0])
			return 1
		}
		entries = r.Entries
	default:
		fmt.Fprintln(opts.Stderr, "dex ls: too many arguments")
		return 2
	}

	if opts.JSON {
		return emitJSON(opts.Stdout, opts.Stderr, entries)
	}
	return emitHuman(opts.Stdout, entries)
}

func emitJSON(stdout, stderr io.Writer, entries []model.Entry) int {
	if entries == nil {
		entries = []model.Entry{}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		fmt.Fprintf(stderr, "dex ls: encode: %v\n", err)
		return 1
	}
	return 0
}

func emitHuman(stdout io.Writer, entries []model.Entry) int {
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(empty)")
		return 0
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%-32s  %s  %s\n", e.Slug, e.Kind, e.Label)
	}
	return 0
}
