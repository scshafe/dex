package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writeSearchFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bundled := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"broker-status","label":"Broker status","kind":"info","info":{"content":"x"},"explore":{"description":"Show broker liveness."}},
			{"id":"01HB00000000000000000000E2","slug":"docs","label":"Documentation","kind":"info","info":{"content":"y"}}
		]
	}`
	personal := `{
		"schema_version": 1,
		"id": "01HP00000000000000000000R1",
		"slug": "root",
		"label": "Personal",
		"visibility": "personal",
		"entries": [
			{"id":"01HP00000000000000000000E1","slug":"my-broker-notes","label":"Notes","kind":"info","info":{"content":"z"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(bundled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "personal", "root.json"), []byte(personal), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSubstringMatchesAcrossTiers(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"broker"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got []struct {
		Slug       string `json:"slug"`
		ParentSlug string `json:"parent_slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// broker-status + my-broker-notes = 2
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2: %+v", len(got), got)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"BROKER"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "broker-status") {
		t.Fatalf("expected case-insensitive match; got %q", out.String())
	}
}

func TestSearchNoMatches(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"nonexistent"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("expected []; got %q", out.String())
	}
}

func TestSearchRequiresArg(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("search without arg should error")
	}
}

func TestSearchMatchesExploreDescription(t *testing.T) {
	tmp := t.TempDir()
	writeSearchFixture(t, tmp)
	var out bytes.Buffer
	// "liveness" appears only in broker-status's explore.description.
	exit := cli.RunSearch(cli.SearchOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"liveness"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "broker-status") {
		t.Fatalf("expected match via explore.description; got %q", out.String())
	}
}
