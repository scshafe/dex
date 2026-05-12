package session

import (
	"encoding/json"
	"time"

	"github.com/scshafe/dex/internal/model"
)

// SessionTTL is the sliding inactivity window. Every successful step
// touches LastTouched; sessions older than this are GC'd opportunistically
// on NewSession.
const SessionTTL = 30 * time.Minute

// CursorMode is "browse" when the cursor is at a rolodex (entry_id
// empty) or "entry" when it's on a specific entry.
type CursorMode string

const (
	CursorModeBrowse CursorMode = "browse"
	CursorModeEntry  CursorMode = "entry"
)

// Cursor is "where you are" in the graph.
type Cursor struct {
	RolodexID string     `json:"rolodex_id"`
	Path      string     `json:"path"` // display only — set by /path drills
	EntryID   string     `json:"entry_id,omitempty"`
	Mode      CursorMode `json:"mode"`
}

// PendingConcern is one still-unsatisfied parameter on the current
// command. Surfaced when the cursor's entry is a command and resolve
// hasn't filled the concern's local_id yet.
type PendingConcern struct {
	LocalID   string   `json:"local_id"`
	Label     string   `json:"label"`
	Default   string   `json:"default,omitempty"`
	Required  bool     `json:"required"`
	Strict    bool     `json:"strict"`
	Validator string   `json:"validator,omitempty"`  // not enforced in v1
	DependsOn []string `json:"depends_on,omitempty"` // not enforced in v1
}

// State is the full session, persisted as JSON.
type State struct {
	ID              string            `json:"id"` // "ses_" + ULID
	Cursor          Cursor            `json:"cursor"`
	Resolved        map[string]string `json:"resolved"`
	PendingConcerns []PendingConcern  `json:"pending_concerns"`
	Version         int               `json:"version"`
	CreatedAt       time.Time         `json:"created_at"`
	LastTouched     time.Time         `json:"last_touched"`
	// PreviousCursors is the back-stack used by the "pop" action.
	// Pushed on drill, popped on pop. Empty stack + pop is a no-op
	// that returns the current envelope unchanged.
	PreviousCursors []Cursor `json:"previous_cursors"`
}

// Action is the input to Apply. Exactly one of the typed fields is
// expected to be set per call; the reducer dispatches on the Type
// string.
type Action struct {
	Type    string `json:"action"`
	Target  string `json:"target,omitempty"`  // for drill
	Concern string `json:"concern,omitempty"` // for resolve
	Value   string `json:"value,omitempty"`   // for resolve
}

const (
	ActionDrill    = "drill"
	ActionPop      = "pop"
	ActionActivate = "activate"
	ActionResolve  = "resolve"
)

// EffectType discriminates the Effect union. Only one of the typed
// payload fields is set per effect.
type EffectType string

const (
	EffectSpawn        EffectType = "spawn"
	EffectStdout       EffectType = "stdout"
	EffectCloseSession EffectType = "close_session"
)

type Effect struct {
	Type         EffectType `json:"type"`
	ShellCommand string     `json:"shell_command,omitempty"` // spawn
	Content      string     `json:"content,omitempty"`       // stdout
}

// ErrorCode is the closed set from the architect's Q1 response.
type ErrorCode string

const (
	ErrInvalidTarget      ErrorCode = "INVALID_TARGET"
	ErrUnresolvedRequired ErrorCode = "UNRESOLVED_REQUIRED"
	ErrValidatorFailed    ErrorCode = "VALIDATOR_FAILED"
	ErrStrictViolation    ErrorCode = "STRICT_VIOLATION"
	ErrNotFound           ErrorCode = "NOT_FOUND"
	ErrStaleSession       ErrorCode = "STALE_SESSION"
	ErrProviderFailed     ErrorCode = "PROVIDER_FAILED"
	ErrSchemaError        ErrorCode = "SCHEMA_ERROR"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Concern string    `json:"concern,omitempty"`
	Hint    string    `json:"hint,omitempty"`
}

// Envelope is what the reducer returns on every step, success or
// failure. ok=false sets Error and leaves Effects empty/State at the
// pre-action snapshot. view is a render hint; this slice always
// returns nil.
type Envelope struct {
	OK      bool             `json:"ok"`
	Session SessionView      `json:"session"`
	View    *json.RawMessage `json:"view"` // always nil in this slice
	Effects []Effect         `json:"effects"`
	Error   *Error           `json:"error"`
}

// SessionView is the externally-visible projection of State that
// every envelope ships. Mirrors the architect's Q1 envelope shape.
// Excluded vs State: created_at, previous_cursors (internal), the
// concern back-stack stays in State.
type SessionView struct {
	ID              string            `json:"id"`
	Cursor          Cursor            `json:"cursor"`
	Resolved        map[string]string `json:"resolved"`
	PendingConcerns []PendingConcern  `json:"pending_concerns"`
	Version         int               `json:"version"`
}

// _ keeps the model import live; later reducer files use it via the
// package-level import. Refactor-resistant.
var _ = model.KindCommand
