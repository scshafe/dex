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

type ExploreOpts struct {
	StoreRoot string
	JSON      bool
	Stdout    io.Writer
	Stderr    io.Writer
}

// exploreOutput is the structured payload `dex explore` emits. It's
// stable enough that agents can rely on it.
type exploreOutput struct {
	ID         string           `json:"id"`
	Slug       string           `json:"slug"`
	Label      string           `json:"label"`
	Kind       model.EntryKind  `json:"kind"`
	Context    string           `json:"context,omitempty"`
	Explore    *model.Explore   `json:"explore,omitempty"`
	Concerns   []model.Concern  `json:"concerns"`
	ParentSlug string           `json:"parent_slug"`
	ParentID   string           `json:"parent_id"`
}

// RunExplore implements `dex explore <ULID|/path>`. Prints the entry's
// self-description (explore block + concerns for command kind).
func RunExplore(opts ExploreOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex explore: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) != 1 {
		fmt.Fprintln(opts.Stderr, "dex explore: requires exactly one argument (<ULID> or </path>)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
		return 1
	}

	arg := argv[0]
	var entry model.Entry
	var parent model.Rolodex

	if strings.HasPrefix(arg, "/") {
		root, err := s.MergedRoot()
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
			return 1
		}
		result, err := path.Resolve(s, root, arg)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
			return 1
		}
		entry = result.Entry
		parent = result.ParentRolodex
	} else {
		e, p, ok, err := s.LookupEntryByID(arg)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(opts.Stderr, "dex explore: entry %q not found\n", arg)
			return 1
		}
		entry = e
		parent = p
	}

	out := exploreOutput{
		ID:         entry.ID,
		Slug:       entry.Slug,
		Label:      entry.Label,
		Kind:       entry.Kind,
		Context:    entry.Context,
		Explore:    entry.Explore,
		Concerns:   []model.Concern{},
		ParentSlug: parent.Slug,
		ParentID:   parent.ID,
	}
	if entry.Kind == model.KindCommand && entry.Command != nil {
		out.Concerns = entry.Command.Concerns
	}

	if opts.JSON {
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(opts.Stderr, "dex explore: encode: %v\n", err)
			return 1
		}
		return 0
	}
	return emitExploreHuman(opts.Stdout, out, entry)
}

func emitExploreHuman(w io.Writer, out exploreOutput, entry model.Entry) int {
	fmt.Fprintf(w, "%s  [%s]\n", out.Slug, out.Kind)
	if out.Label != "" {
		fmt.Fprintf(w, "  label:   %s\n", out.Label)
	}
	if out.Context != "" {
		fmt.Fprintf(w, "  context: %s\n", out.Context)
	}
	fmt.Fprintf(w, "  id:      %s\n", out.ID)
	fmt.Fprintf(w, "  parent:  %s (%s)\n", out.ParentSlug, out.ParentID)
	if out.Explore != nil {
		if out.Explore.Description != "" {
			fmt.Fprintf(w, "\n%s\n", out.Explore.Description)
		}
		if len(out.Explore.Examples) > 0 {
			fmt.Fprintln(w, "\nExamples:")
			for _, ex := range out.Explore.Examples {
				fmt.Fprintf(w, "  # %s\n  %s\n", ex.Description, ex.Invocation)
			}
		}
		if out.Explore.Notes != "" {
			fmt.Fprintf(w, "\nNotes: %s\n", out.Explore.Notes)
		}
	}
	if entry.Kind == model.KindCommand && entry.Command != nil {
		fmt.Fprintf(w, "\nTemplate: %s\n", entry.Command.Template)
		if len(entry.Command.Concerns) > 0 {
			fmt.Fprintln(w, "Concerns:")
			for _, c := range entry.Command.Concerns {
				req := ""
				if c.Required {
					req = " (required)"
				}
				fmt.Fprintf(w, "  %s — %s%s\n", c.LocalID, c.Label, req)
			}
		}
	}
	return 0
}
