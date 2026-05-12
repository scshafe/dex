package model

import (
	"encoding/json"
	"testing"
)

const sampleRolodex = `{
  "schema_version": 1,
  "id": "01HQ7AB000000000000000ABCD",
  "slug": "broker-providers",
  "label": "Broker providers",
  "visibility": "bundled",
  "entries": [
    {
      "id": "01HQ7AB000000000000000ENT1",
      "slug": "broker-status",
      "label": "Broker status",
      "kind": "command",
      "command": {
        "template": "wm broker status --provider {provider}",
        "concerns": [
          {
            "id": "01HQ7AB000000000000000CON1",
            "local_id": "provider",
            "slug": "provider-concern",
            "label": "Which provider?",
            "rolodex": { "to": "01HQ7AB000000000000000XYZ1" },
            "required": false,
            "strict": false
          }
        ]
      },
      "explore": {
        "description": "Snapshot of provider freshness.",
        "examples": [
          {"description": "All providers", "invocation": "wm broker status"}
        ]
      }
    },
    {
      "id": "01HQ7AB000000000000000ENT2",
      "slug": "tools",
      "label": "Tools",
      "kind": "pointer",
      "pointer": { "to": "01HQ7AB000000000000000XYZ2" }
    },
    {
      "id": "01HQ7AB000000000000000ENT3",
      "slug": "readme",
      "label": "Readme",
      "kind": "info",
      "info": { "content": "Hello." }
    }
  ]
}`

func TestRolodexRoundTrip(t *testing.T) {
	var r Rolodex
	if err := json.Unmarshal([]byte(sampleRolodex), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.SchemaVersion != 1 {
		t.Fatalf("schema_version: got %d want 1", r.SchemaVersion)
	}
	if got := len(r.Entries); got != 3 {
		t.Fatalf("entries: got %d want 3", got)
	}

	cmd := r.Entries[0]
	if cmd.Kind != KindCommand {
		t.Fatalf("entry[0].kind: got %q want command", cmd.Kind)
	}
	if cmd.Command == nil {
		t.Fatal("entry[0].command nil")
	}
	if len(cmd.Command.Concerns) != 1 {
		t.Fatalf("entry[0].command.concerns: got %d want 1", len(cmd.Command.Concerns))
	}
	concern := cmd.Command.Concerns[0]
	if concern.LocalID != "provider" {
		t.Fatalf("concern.local_id: got %q want provider", concern.LocalID)
	}
	if concern.ID != "01HQ7AB000000000000000CON1" {
		t.Fatalf("concern.id: got %q", concern.ID)
	}
	// NodeCore is embedded; promoted fields should be addressable directly.
	if concern.Label != "Which provider?" {
		t.Fatalf("concern.label: got %q", concern.Label)
	}

	ptr := r.Entries[1]
	if ptr.Kind != KindPointer || ptr.Pointer == nil || ptr.Pointer.To == "" {
		t.Fatalf("entry[1] pointer payload missing")
	}

	info := r.Entries[2]
	if info.Kind != KindInfo || info.Info == nil || info.Info.Content != "Hello." {
		t.Fatalf("entry[2] info payload wrong")
	}

	// Re-marshal and re-unmarshal; check it round-trips structurally.
	b, err := json.Marshal(&r)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var r2 Rolodex
	if err := json.Unmarshal(b, &r2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if r2.ID != r.ID || len(r2.Entries) != len(r.Entries) {
		t.Fatalf("round-trip diverged")
	}
}
