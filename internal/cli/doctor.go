package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type DoctorOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunDoctor implements `dex doctor`. Walks every rolodex via LoadAll
// (which itself schema-validates each file on read), then scans for
// dangling references — any pointer.to or concern.rolodex.to that
// names a rolodex ULID we haven't seen.
//
// Exit 0 if the store is clean; exit 1 if any issue is reported.
func RunDoctor(opts DoctorOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex doctor: store root not set (use DEX_STORE)")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex doctor: %v\n", err)
		return 1
	}
	rolodexes, err := s.LoadAll()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex doctor: %v\n", err)
		return 1
	}

	known := map[string]bool{}
	for _, r := range rolodexes {
		known[r.ID] = true
	}

	var findings []string
	for _, r := range rolodexes {
		for _, e := range r.Entries {
			if e.Kind == model.KindPointer && e.Pointer != nil {
				if !known[e.Pointer.To] {
					findings = append(findings,
						fmt.Sprintf("dangling pointer: rolodex %s/%s entry %s/%s → %s",
							r.Visibility, r.Slug, e.Slug, e.ID, e.Pointer.To))
				}
			}
			if e.Kind == model.KindCommand && e.Command != nil {
				for _, c := range e.Command.Concerns {
					if c.Rolodex != nil && !known[c.Rolodex.To] {
						findings = append(findings,
							fmt.Sprintf("dangling concern rolodex: rolodex %s/%s entry %s/%s concern %s → %s",
								r.Visibility, r.Slug, e.Slug, e.ID, c.LocalID, c.Rolodex.To))
					}
				}
			}
		}
	}

	if len(findings) == 0 {
		fmt.Fprintf(opts.Stdout, "dex doctor: store is clean (%d rolodexes checked, no issues)\n", len(rolodexes))
		return 0
	}
	fmt.Fprintf(opts.Stderr, "dex doctor: %d issue(s) found:\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(opts.Stderr, "  - %s\n", f)
	}
	return 1
}
