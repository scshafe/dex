// Package path resolves dex paths (e.g. "/commands/broker") to entries.
//
// Architect's Q3: paths are pure sugar over uuid; the engine sees only
// uuids; paths are canonicalized at the CLI boundary. Resolution walks
// the slug chain starting at the merged root, following `pointer`
// entries mid-path; the final segment can be any kind.
package path

import (
	"errors"
	"fmt"
	"strings"

	"github.com/scshafe/dex/internal/model"
)

// Resolver looks up a Rolodex by its ULID. The path package depends on
// this narrow interface rather than the store package directly, so it
// stays algorithm-only and is reusable.
type Resolver interface {
	LookupByID(id string) (model.Rolodex, bool, error)
}

// Result is the outcome of a successful Resolve.
type Result struct {
	// Entry is the final-segment entry, regardless of kind.
	Entry model.Entry
	// ParentRolodex is the rolodex that contains Entry. For first-segment
	// paths this is the merged root passed into Resolve.
	ParentRolodex model.Rolodex
}

var (
	// ErrNotFound is returned when a path segment doesn't match any slug.
	ErrNotFound = errors.New("path: not found")
	// ErrTraversesNonPointer is returned when a mid-path segment exists
	// but is not a pointer entry (so resolution cannot continue).
	ErrTraversesNonPointer = errors.New("path: traverses non-pointer entry")
	// ErrCycle is returned when path resolution exceeds the depth cap,
	// which catches both pointer cycles and pathologically deep chains.
	ErrCycle = errors.New("path: cycle or unreasonably deep chain")
	// ErrSyntax is returned when the path doesn't start with "/" or is empty.
	ErrSyntax = errors.New("path: invalid syntax")
)

// MaxDepth is the hard cap on resolution hops. Each segment counts as
// one hop. A path that exceeds the cap returns ErrCycle — which catches
// both genuine pointer cycles and pathologically deep chains.
const MaxDepth = 32

// Resolve walks the path from mergedRoot, following pointer entries
// mid-path and returning the final-segment entry (any kind). Paths must
// start with "/". Trailing slashes are ignored.
func Resolve(r Resolver, mergedRoot model.Rolodex, p string) (Result, error) {
	if !strings.HasPrefix(p, "/") {
		return Result{}, fmt.Errorf("%w: must start with %q (got %q)", ErrSyntax, "/", p)
	}
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return Result{}, fmt.Errorf("%w: empty path", ErrSyntax)
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) > MaxDepth {
		return Result{}, fmt.Errorf("%w: %d segments exceeds cap of %d", ErrCycle, len(segments), MaxDepth)
	}

	currentRolodex := mergedRoot
	for i, seg := range segments {
		var entry model.Entry
		found := false
		for _, e := range currentRolodex.Entries {
			if e.Slug == seg {
				entry = e
				found = true
				break
			}
		}
		if !found {
			return Result{}, fmt.Errorf("%w: %q in %s", ErrNotFound, seg, currentRolodex.Slug)
		}

		// Final segment: return regardless of kind.
		if i == len(segments)-1 {
			return Result{Entry: entry, ParentRolodex: currentRolodex}, nil
		}

		// Mid-path: must be a pointer to continue traversal.
		if entry.Kind != model.KindPointer {
			return Result{}, fmt.Errorf("%w: %q is %s", ErrTraversesNonPointer, seg, entry.Kind)
		}
		if entry.Pointer == nil {
			return Result{}, fmt.Errorf("%w: pointer entry %q has nil payload", ErrTraversesNonPointer, seg)
		}

		next, ok, err := r.LookupByID(entry.Pointer.To)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, fmt.Errorf("%w: pointer target %q", ErrNotFound, entry.Pointer.To)
		}
		currentRolodex = next
	}

	// Unreachable: the loop always returns or errors.
	return Result{}, fmt.Errorf("%w: unreachable", ErrSyntax)
}
