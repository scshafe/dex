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

func writeRmFixture(t *testing.T, root string) (entryID string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	rolodex := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"keep","label":"keep","kind":"info","info":{"content":"a"}},
			{"id":"01HB00000000000000000000E2","slug":"remove","label":"remove","kind":"info","info":{"content":"b"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000E2"
}

func TestRmEntry(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeRmFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunRm(cli.RmOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{entryID})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"entries"`
	}
	_ = json.Unmarshal(b, &got)
	if len(got.Entries) != 1 {
		t.Fatalf("expected 1 remaining entry, got %d", len(got.Entries))
	}
	if got.Entries[0].Slug != "keep" {
		t.Fatalf("kept the wrong entry: %+v", got.Entries)
	}
}

func TestRmEntryNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeRmFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunRm(cli.RmOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected error for unknown entry")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestRmRequiresEntryID(t *testing.T) {
	tmp := t.TempDir()
	writeRmFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunRm(cli.RmOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("expected error when entry id is missing")
	}
}
