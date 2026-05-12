package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type EditOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunEdit implements `dex edit <entry-ULID> [flags...]`. Editable fields:
// --label, --context, plus kind-specific (--content for info,
// --pointer-to for pointer). Concern editing on commands is deferred.
func RunEdit(opts EditOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex edit: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) < 1 {
		fmt.Fprintln(opts.Stderr, "dex edit: first argument must be the entry ULID")
		return 2
	}
	entryID := argv[0]

	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	label := fs.String("label", "", "new label")
	context := fs.String("context", "", "new context")
	content := fs.String("content", "", "new info content (info kind only)")
	pointerTo := fs.String("pointer-to", "", "new pointer target (pointer kind only)")
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}

	labelSet := isFlagSet(fs, "label")
	contextSet := isFlagSet(fs, "context")
	contentSet := isFlagSet(fs, "content")
	pointerSet := isFlagSet(fs, "pointer-to")

	if !labelSet && !contextSet && !contentSet && !pointerSet {
		fmt.Fprintln(opts.Stderr, "dex edit: at least one field flag must be set")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex edit: %v\n", err)
		return 1
	}
	entry, parent, ok, err := s.LookupEntryByID(entryID)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex edit: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex edit: entry %q not found\n", entryID)
		return 1
	}

	if contentSet && entry.Kind != model.KindInfo {
		fmt.Fprintf(opts.Stderr, "dex edit: --content only applies to info entries (got %s)\n", entry.Kind)
		return 2
	}
	if pointerSet && entry.Kind != model.KindPointer {
		fmt.Fprintf(opts.Stderr, "dex edit: --pointer-to only applies to pointer entries (got %s)\n", entry.Kind)
		return 2
	}

	if labelSet {
		entry.Label = *label
	}
	if contextSet {
		entry.Context = *context
	}
	if contentSet {
		if entry.Info == nil {
			entry.Info = &model.InfoPayload{}
		}
		entry.Info.Content = *content
	}
	if pointerSet {
		entry.Pointer = &model.PointerPayload{To: *pointerTo}
	}

	for i, e := range parent.Entries {
		if e.ID == entryID {
			parent.Entries[i] = entry
			break
		}
	}

	if err := s.WriteRolodex(parent); err != nil {
		fmt.Fprintf(opts.Stderr, "dex edit: %v\n", err)
		return 1
	}
	return 0
}

// isFlagSet reports whether the named flag was explicitly set (vs left at zero).
func isFlagSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}
