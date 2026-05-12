// Package cli implements the dex command verbs. Each verb is a Run<Verb>
// function that takes an Opts struct and an argv tail; the main package
// wires them into the verb dispatch.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/path"
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
		arg := argv[0]
		if strings.HasPrefix(arg, "/") {
			var err error
			entries, err = resolvePath(s, arg, opts.Stderr)
			if err != nil {
				return 1
			}
		} else {
			r, ok, err := s.LookupByID(arg)
			if err != nil {
				fmt.Fprintf(opts.Stderr, "dex ls: %v\n", err)
				return 1
			}
			if !ok {
				fmt.Fprintf(opts.Stderr, "dex ls: rolodex %q not found\n", arg)
				return 1
			}
			entries = r.Entries
		}
	default:
		fmt.Fprintln(opts.Stderr, "dex ls: too many arguments")
		return 2
	}

	if opts.JSON {
		return emitJSON(opts.Stdout, opts.Stderr, entries)
	}
	return emitHuman(opts.Stdout, entries)
}

// resolvePath handles the path arm of `dex ls`. Special-cases "/" as
// "list merged root"; otherwise walks the path via internal/path and
// drills if the final entry is a pointer.
func resolvePath(s *store.Store, p string, stderr io.Writer) ([]model.Entry, error) {
	root, err := s.MergedRoot()
	if err != nil {
		fmt.Fprintf(stderr, "dex ls: %v\n", err)
		return nil, err
	}
	if p == "/" {
		return root.Entries, nil
	}

	result, err := path.Resolve(s, root, p)
	if err != nil {
		fmt.Fprintf(stderr, "dex ls: %v\n", err)
		return nil, err
	}
	if result.Entry.Kind != model.KindPointer {
		fmt.Fprintf(stderr,
			"dex ls: %q is a %s entry; use `dex explore` or `dex activate` instead\n",
			p, result.Entry.Kind)
		return nil, fmt.Errorf("not a pointer")
	}
	if result.Entry.Pointer == nil {
		fmt.Fprintf(stderr, "dex ls: pointer entry %q has nil payload\n", p)
		return nil, fmt.Errorf("nil pointer payload")
	}
	target, ok, err := s.LookupByID(result.Entry.Pointer.To)
	if err != nil {
		fmt.Fprintf(stderr, "dex ls: %v\n", err)
		return nil, err
	}
	if !ok {
		fmt.Fprintf(stderr, "dex ls: dangling pointer at %q (target %q)\n",
			p, result.Entry.Pointer.To)
		return nil, fmt.Errorf("dangling pointer")
	}
	return target.Entries, nil
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
