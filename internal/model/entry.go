package model

type EntryKind string

const (
	KindPointer EntryKind = "pointer"
	KindCommand EntryKind = "command"
	KindInfo    EntryKind = "info"
)

type Entry struct {
	NodeCore
	Kind    EntryKind       `json:"kind"`
	Pointer *PointerPayload `json:"pointer,omitempty"`
	Command *CommandPayload `json:"command,omitempty"`
	Info    *InfoPayload    `json:"info,omitempty"`
}

type PointerPayload struct {
	To string `json:"to"` // ULID of target rolodex
}

type CommandPayload struct {
	Template string    `json:"template"`
	Concerns []Concern `json:"concerns,omitempty"`
}

type InfoPayload struct {
	// Exactly one of Content or Provider must be set. Provider is a
	// registered script-id (architect's landmine #1) — not a free-form
	// shell string.
	Content  string `json:"content,omitempty"`
	Provider string `json:"provider,omitempty"`
}
