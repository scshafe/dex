// Package model contains the dex data-model types.
//
// Entry and Concern embed a shared NodeCore so that the v2 schema collapse
// (entries + concerns into a unified Node type) is a re-tagging, not a
// re-shaping. See docs/design.md "Open Schema Tension" and the architect's
// Q2 response in handoff context.
package model

import "fmt"

type Visibility string

const (
	VisibilityBundled   Visibility = "bundled"
	VisibilityPersonal  Visibility = "personal"
	VisibilityPrivate   Visibility = "private"
	VisibilityEphemeral Visibility = "ephemeral"
)

func (v Visibility) Validate() error {
	switch v {
	case VisibilityBundled, VisibilityPersonal, VisibilityPrivate, VisibilityEphemeral:
		return nil
	}
	return fmt.Errorf("invalid visibility %q", string(v))
}

// Precedence returns the collision-resolution order for merged-root assembly.
// Higher wins; private > personal > bundled encodes inverse trust order so
// user customization shadows bundled defaults. Ephemeral is not part of
// the merged root and returns 0.
func (v Visibility) Precedence() int {
	switch v {
	case VisibilityBundled:
		return 1
	case VisibilityPersonal:
		return 2
	case VisibilityPrivate:
		return 3
	}
	return 0
}
