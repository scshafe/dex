package cli

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/scshafe/dex/internal/model"
	"github.com/scshafe/dex/internal/store"
)

type AddOpts struct {
	StoreRoot string
	Stdout    io.Writer
	Stderr    io.Writer
}

// RunAdd implements `dex add --parent <ULID> ...`. Two modes:
//
//   - Flag mode: --slug, --label, --kind, plus kind-specific payload
//     (--pointer-to, or --content). Pointer + info-content only in v1.
//   - JSON mode: --from-json <path|-> reads a full Entry JSON. The
//     agent-write path; supports command-kind with concerns.
//
// In both modes, --id is optional (a ULID is generated if omitted) and
// the new entry's id is printed to stdout.
func RunAdd(opts AddOpts, argv []string) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.StoreRoot == "" {
		fmt.Fprintln(opts.Stderr, "dex add: store root not set (use DEX_STORE)")
		return 2
	}

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(opts.Stderr)
	parent := fs.String("parent", "", "ULID of the rolodex to add to (required)")
	fromJSON := fs.String("from-json", "", "path to a JSON entry file, or '-' for stdin")
	id := fs.String("id", "", "ULID for the new entry (default: generated)")
	slug := fs.String("slug", "", "slug for the new entry (flag mode)")
	label := fs.String("label", "", "label for the new entry (flag mode)")
	context := fs.String("context", "", "optional context string")
	kind := fs.String("kind", "", "pointer | info (flag mode)")
	pointerTo := fs.String("pointer-to", "", "target ULID (when --kind=pointer)")
	content := fs.String("content", "", "info content (when --kind=info)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	if *parent == "" {
		fmt.Fprintln(opts.Stderr, "dex add: --parent is required")
		return 2
	}

	s, err := store.Open(opts.StoreRoot)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex add: %v\n", err)
		return 1
	}

	parentR, ok, err := s.LookupByID(*parent)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "dex add: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(opts.Stderr, "dex add: parent rolodex %q not found\n", *parent)
		return 1
	}

	var entry model.Entry
	if *fromJSON != "" {
		entry, err = readEntryJSON(*fromJSON, opts.Stderr)
		if err != nil {
			return 1
		}
	} else {
		entry, err = buildEntryFromFlags(*slug, *label, *context, *kind, *pointerTo, *content, opts.Stderr)
		if err != nil {
			return 2
		}
	}

	if *id != "" {
		entry.ID = *id
	}
	if entry.ID == "" {
		entry.ID = newULID()
	}

	for _, e := range parentR.Entries {
		if e.Slug == entry.Slug {
			fmt.Fprintf(opts.Stderr,
				"dex add: duplicate slug %q already exists in parent (use 'dex edit' to modify)\n",
				entry.Slug)
			return 1
		}
	}

	parentR.Entries = append(parentR.Entries, entry)
	if err := s.WriteRolodex(parentR); err != nil {
		fmt.Fprintf(opts.Stderr, "dex add: %v\n", err)
		return 1
	}

	fmt.Fprintln(opts.Stdout, entry.ID)
	return 0
}

func buildEntryFromFlags(slug, label, context, kind, pointerTo, content string, stderr io.Writer) (model.Entry, error) {
	if slug == "" || label == "" || kind == "" {
		fmt.Fprintln(stderr, "dex add: --slug, --label, and --kind are required in flag mode")
		return model.Entry{}, fmt.Errorf("missing required flags")
	}
	entry := model.Entry{
		NodeCore: model.NodeCore{Slug: slug, Label: label, Context: context},
		Kind:     model.EntryKind(kind),
	}
	switch model.EntryKind(kind) {
	case model.KindPointer:
		if pointerTo == "" {
			fmt.Fprintln(stderr, "dex add: --pointer-to is required when --kind=pointer")
			return model.Entry{}, fmt.Errorf("missing --pointer-to")
		}
		entry.Pointer = &model.PointerPayload{To: pointerTo}
	case model.KindInfo:
		if content == "" {
			fmt.Fprintln(stderr, "dex add: --content is required when --kind=info (provider mode not supported in flag-based add)")
			return model.Entry{}, fmt.Errorf("missing --content")
		}
		entry.Info = &model.InfoPayload{Content: content}
	case model.KindCommand:
		fmt.Fprintln(stderr, "dex add: --kind=command not supported in flag mode; use --from-json")
		return model.Entry{}, fmt.Errorf("command kind requires --from-json")
	default:
		fmt.Fprintf(stderr, "dex add: unknown kind %q (want pointer or info)\n", kind)
		return model.Entry{}, fmt.Errorf("unknown kind")
	}
	return entry, nil
}

func readEntryJSON(pathOrDash string, stderr io.Writer) (model.Entry, error) {
	var b []byte
	var err error
	if pathOrDash == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(pathOrDash)
	}
	if err != nil {
		fmt.Fprintf(stderr, "dex add: read entry json: %v\n", err)
		return model.Entry{}, err
	}
	var entry model.Entry
	if err := json.Unmarshal(b, &entry); err != nil {
		fmt.Fprintf(stderr, "dex add: parse entry json: %v\n", err)
		return model.Entry{}, err
	}
	return entry, nil
}

// newULID generates a fresh ULID using crypto/rand entropy.
func newULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy{}).String()
}

// ulidEntropy adapts crypto/rand for ulid.MustNew.
type ulidEntropy struct{}

func (ulidEntropy) Read(p []byte) (int, error) { return rand.Read(p) }
