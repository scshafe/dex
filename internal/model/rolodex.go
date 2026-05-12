package model

// Rolodex is the top-level container. schema_version is on the rolodex
// only — never on individual entries or concerns (architect's landmine #3:
// per-file versioning, not per-node, to keep migrations sane).
type Rolodex struct {
	SchemaVersion int        `json:"schema_version"`
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Label         string     `json:"label"`
	Context       string     `json:"context,omitempty"`
	Visibility    Visibility `json:"visibility"`
	Entries       []Entry    `json:"entries"`
}
