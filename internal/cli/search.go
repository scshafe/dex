package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type SearchOpts struct {
	StoreRoot string
	JSON      bool
	Stdout    io.Writer
	Stderr    io.Writer
}

type searchMatch struct {
	ID         string           `json:"id"`
	Slug       string           `json:"slug"`
	Label      string           `json:"label"`
	Kind       model.EntryKind  `json:"kind"`
	ParentID   string           `json:"parent_id"`
	ParentSlug string           `json:"parent_slug"`
	Visibility model.Visibility `json:"visibility"`
}

// RunSearch implements `dex search <query>`. Case-insensitive substring
// match over slug, label, context, and explore.description across all
// rolodexes in every tier.
func RunSearch(opts SearchOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex search: store root not set (use DEX_STORE)")
		return 2
	}
	if len(argv) != 1 {
		fmt.Fprintln(opts.Stderr, "dex search: requires exactly one query argument")
		return 2
	}
	query := strings.ToLower(argv[0])

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex search: %v\n", err)
		return 1
	}
	rolodexes, err := s.LoadAll()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex search: %v\n", err)
		return 1
	}

	matches := []searchMatch{}
	for _, r := range rolodexes {
		for _, e := range r.Entries {
			if entryMatches(e, query) {
				matches = append(matches, searchMatch{
					ID:         e.ID,
					Slug:       e.Slug,
					Label:      e.Label,
					Kind:       e.Kind,
					ParentID:   r.ID,
					ParentSlug: r.Slug,
					Visibility: r.Visibility,
				})
			}
		}
	}

	if opts.JSON {
		enc := json.NewEncoder(opts.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(matches); err != nil {
			fmt.Fprintf(opts.Stderr, "dex search: encode: %v\n", err)
			return 1
		}
		return 0
	}
	if len(matches) == 0 {
		fmt.Fprintln(opts.Stdout, "(no matches)")
		return 0
	}
	for _, m := range matches {
		fmt.Fprintf(opts.Stdout, "%-32s  %s  %s/%s  [%s]\n",
			m.Slug, m.Kind, m.ParentSlug, m.ID, m.Visibility)
	}
	return 0
}

// entryMatches checks whether any of slug, label, context, or
// explore.description (case-folded) contains the lowercase query.
func entryMatches(e model.Entry, lowerQuery string) bool {
	if strings.Contains(strings.ToLower(e.Slug), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Label), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Context), lowerQuery) {
		return true
	}
	if e.Explore != nil && strings.Contains(strings.ToLower(e.Explore.Description), lowerQuery) {
		return true
	}
	return false
}
