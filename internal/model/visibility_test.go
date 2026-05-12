package model

import (
	"encoding/json"
	"testing"
)

func TestVisibilityRoundTrip(t *testing.T) {
	cases := []Visibility{
		VisibilityBundled, VisibilityPersonal, VisibilityPrivate, VisibilityEphemeral,
	}
	for _, v := range cases {
		t.Run(string(v), func(t *testing.T) {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Visibility
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != v {
				t.Fatalf("round-trip: got %q want %q", got, v)
			}
		})
	}
}

func TestVisibilityValidate(t *testing.T) {
	if err := VisibilityBundled.Validate(); err != nil {
		t.Fatalf("expected bundled to validate: %v", err)
	}
	if err := Visibility("nonsense").Validate(); err == nil {
		t.Fatalf("expected invalid visibility to error")
	}
}

func TestVisibilityPrecedence(t *testing.T) {
	// Q3 collision rule: private > personal > bundled.
	// Ephemeral is not part of the merged root.
	tiers := []Visibility{VisibilityBundled, VisibilityPersonal, VisibilityPrivate}
	for i := 0; i < len(tiers)-1; i++ {
		if tiers[i].Precedence() >= tiers[i+1].Precedence() {
			t.Fatalf("expected ascending precedence at index %d: %v >= %v",
				i, tiers[i], tiers[i+1])
		}
	}
}
