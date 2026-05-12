package model

// NodeCore is the identity-and-prose substrate shared by Entry and Concern.
// Embedding promotes the JSON fields to the outer object, so both types
// serialize with id/slug/label/context/explore at the top level.
//
// Architect's Q2: this is the only sharing v1 does between Entry and Concern.
// Activation contracts (kind, pointer/command/info, rolodex pointer for
// concerns) stay separate; v2 introduces a `produces` discriminator that
// re-tags without re-shaping.
type NodeCore struct {
	ID      string   `json:"id"`
	Slug    string   `json:"slug"`
	Label   string   `json:"label"`
	Context string   `json:"context,omitempty"`
	Explore *Explore `json:"explore,omitempty"`
}

type Explore struct {
	Description string    `json:"description,omitempty"`
	Examples    []Example `json:"examples,omitempty"`
	Notes       string    `json:"notes,omitempty"`
}

type Example struct {
	Description string `json:"description"`
	Invocation  string `json:"invocation"`
}
