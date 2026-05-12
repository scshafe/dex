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

type ActivateOpts struct {
	StoreRoot string
	JSON      bool
	DryRun    bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunActivate implements `dex activate <ULID|/path> [concern=value]...`.
// Kind-dispatched:
//   - pointer: drills (lists target rolodex's entries; same as `dex ls`)
//   - info with content: prints content
//   - info with provider: errors (v1 — providers deferred)
//   - command: assembles template, validates concerns, execs (Tasks 6/7)
func RunActivate(opts ActivateOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex activate: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) < 1 {
		fmt.Fprintln(opts.Stderr, "dex activate: requires an entry argument (<ULID> or </path>)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex activate: %v\n", err)
		return 1
	}

	entry, _, err := resolveActivateTarget(s, argv[0], opts.Stderr)
	if err != nil {
		return 1
	}

	switch entry.Kind {
	case model.KindPointer:
		return activatePointer(s, entry, opts)
	case model.KindInfo:
		return activateInfo(entry, opts)
	case model.KindCommand:
		// Tasks 6 + 7 fill this in.
		fmt.Fprintln(opts.Stderr, "dex activate: command kind not yet implemented")
		return 2
	default:
		fmt.Fprintf(opts.Stderr, "dex activate: unknown entry kind %q\n", entry.Kind)
		return 1
	}
}

func resolveActivateTarget(s *store.Store, arg string, stderr io.Writer) (model.Entry, model.Rolodex, error) {
	if strings.HasPrefix(arg, "/") {
		root, err := s.MergedRoot()
		if err != nil {
			fmt.Fprintf(stderr, "dex activate: %v\n", err)
			return model.Entry{}, model.Rolodex{}, err
		}
		result, err := path.Resolve(s, root, arg)
		if err != nil {
			fmt.Fprintf(stderr, "dex activate: %v\n", err)
			return model.Entry{}, model.Rolodex{}, err
		}
		return result.Entry, result.ParentRolodex, nil
	}
	e, p, ok, err := s.LookupEntryByID(arg)
	if err != nil {
		fmt.Fprintf(stderr, "dex activate: %v\n", err)
		return model.Entry{}, model.Rolodex{}, err
	}
	if !ok {
		err := fmt.Errorf("entry %q not found", arg)
		fmt.Fprintf(stderr, "dex activate: %v\n", err)
		return model.Entry{}, model.Rolodex{}, err
	}
	return e, p, nil
}

func activatePointer(s *store.Store, entry model.Entry, opts ActivateOpts) int {
	if entry.Pointer == nil {
		fmt.Fprintf(opts.Stderr, "dex activate: pointer entry %q has nil payload\n", entry.Slug)
		return 1
	}
	target, ok, err := s.LookupByID(entry.Pointer.To)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex activate: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex activate: dangling pointer at %q (target %q)\n",
			entry.Slug, entry.Pointer.To)
		return 1
	}
	if opts.JSON {
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(target.Entries); err != nil {
			fmt.Fprintf(opts.Stderr, "dex activate: encode: %v\n", err)
			return 1
		}
		return 0
	}
	for _, e := range target.Entries {
		fmt.Fprintf(opts.Stdout, "%-32s  %s  %s\n", e.Slug, e.Kind, e.Label)
	}
	return 0
}

func activateInfo(entry model.Entry, opts ActivateOpts) int {
	if entry.Info == nil {
		fmt.Fprintf(opts.Stderr, "dex activate: info entry %q has nil payload\n", entry.Slug)
		return 1
	}
	if entry.Info.Provider != "" {
		fmt.Fprintf(opts.Stderr,
			"dex activate: info entry %q uses provider %q; providers are not implemented in v1\n",
			entry.Slug, entry.Info.Provider)
		return 2
	}
	fmt.Fprintln(opts.Stdout, entry.Info.Content)
	return 0
}
