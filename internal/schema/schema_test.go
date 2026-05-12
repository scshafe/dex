package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/schema"
)

func TestValidFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata/valid")
	if err != nil {
		t.Fatalf("read valid dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no valid fixtures present")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata/valid", e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var parsed any
			if err := json.Unmarshal(b, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := schema.Validate(parsed); err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestInvalidFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata/invalid")
	if err != nil {
		t.Fatalf("read invalid dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no invalid fixtures present")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata/invalid", e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var parsed any
			if err := json.Unmarshal(b, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := schema.Validate(parsed); err == nil {
				t.Fatalf("expected schema violation, got valid")
			}
		})
	}
}
