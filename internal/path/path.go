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

// Resolve walks the path from mergedRoot, returning the final-segment
// entry. Paths must start with "/". Trailing slashes are ignored.
//
// Task 1 supports only single-segment paths. Task 2 extends to
// multi-segment with pointer traversal.
func Resolve(r Resolver, mergedRoot model.Rolodex, p string) (Result, error) {
	if !strings.HasPrefix(p, "/") {
		return Result{}, fmt.Errorf("%w: must start with %q (got %q)", ErrSyntax, "/", p)
	}
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return Result{}, fmt.Errorf("%w: empty path", ErrSyntax)
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) > 1 {
		return Result{}, fmt.Errorf("%w: multi-segment paths not yet supported", ErrSyntax)
	}
	seg := segments[0]
	for _, e := range mergedRoot.Entries {
		if e.Slug == seg {
			return Result{Entry: e, ParentRolodex: mergedRoot}, nil
		}
	}
	return Result{}, fmt.Errorf("%w: %q in %s", ErrNotFound, seg, mergedRoot.Slug)
}
