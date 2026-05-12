package model

// Concern is a parameter-with-suggestions on a command entry.
//
// LocalID is the handle used inside the command template (e.g. `{provider}`);
// ID is the global ULID so v2 can promote concerns to first-class linkable
// nodes without data migration.
type Concern struct {
	NodeCore
	LocalID   string      `json:"local_id"`
	Rolodex   *RolodexRef `json:"rolodex,omitempty"`
	Default   string      `json:"default,omitempty"`
	Required  bool        `json:"required"`
	Strict    bool        `json:"strict"`
	Validator string      `json:"validator,omitempty"` // registered script-id
	DependsOn []string    `json:"depends_on,omitempty"` // local_ids of prior concerns
}

type RolodexRef struct {
	To string `json:"to"`
}
