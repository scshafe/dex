package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
//   - command: assembles template, validates concerns, execs
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
		return activateCommand(entry, argv[1:], opts)
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

// activateCommand handles `dex activate <command-entry> [concern=value]...`.
// Parses concern args (k=v), resolves each declared concern in priority
// order (user-provided > default > error if required), substitutes
// {local_id} placeholders into the template, then either prints
// (--dry-run) or execs via `sh -c`.
func activateCommand(entry model.Entry, concernArgs []string, opts ActivateOpts) int {
	if entry.Command == nil {
		fmt.Fprintf(opts.Stderr, "dex activate: command entry %q has nil payload\n", entry.Slug)
		return 1
	}

	// Parse k=v args into a map.
	provided := map[string]string{}
	for _, a := range concernArgs {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			fmt.Fprintf(opts.Stderr, "dex activate: concern arg %q is not of form key=value\n", a)
			return 2
		}
		provided[k] = v
	}

	// Resolve each declared concern: user-provided > default > error if required.
	resolved := map[string]string{}
	for _, c := range entry.Command.Concerns {
		if v, ok := provided[c.LocalID]; ok {
			resolved[c.LocalID] = v
			continue
		}
		if c.Default != "" {
			resolved[c.LocalID] = c.Default
			continue
		}
		// Optional + no default + not provided → error. Aligns with
		// the session reducer's UNRESOLVED_REQUIRED semantics
		// (silent empty-string substitution was a v0 ergonomics
		// crutch that masked typos).
		fmt.Fprintf(opts.Stderr,
			"dex activate: concern %q has no value (no --concern=value and no default)\n",
			c.LocalID)
		return 1
	}

	// Substitute {local_id} placeholders.
	assembled := entry.Command.Template
	for k, v := range resolved {
		assembled = strings.ReplaceAll(assembled, "{"+k+"}", v)
	}

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, assembled)
		return 0
	}

	cmd := exec.Command("sh", "-c", assembled)
	cmd.Stdin = os.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	err := cmd.Run()
	if err == nil {
		return 0
	}
	// Propagate the child's exit code if available; otherwise generic 1.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(opts.Stderr, "dex activate: exec failed: %v\n", err)
	return 1
}
