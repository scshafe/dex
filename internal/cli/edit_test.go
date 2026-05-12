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

func writeEditFixture(t *testing.T, root string) (entryID string) {
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
			{
				"id": "01HB00000000000000000000E1",
				"slug": "readme",
				"label": "Old label",
				"kind": "info",
				"info": { "content": "old content" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000E1"
}

func TestEditLabelAndContext(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeEditFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{entryID, "--label", "New label", "--context", "new context"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			Label   string `json:"label"`
			Context string `json:"context"`
		} `json:"entries"`
	}
	_ = json.Unmarshal(b, &got)
	if got.Entries[0].Label != "New label" || got.Entries[0].Context != "new context" {
		t.Fatalf("fields not updated: %+v", got.Entries)
	}
}

func TestEditInfoContent(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeEditFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out},
		[]string{entryID, "--content", "new content"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	if !strings.Contains(string(b), "new content") {
		t.Fatalf("content not updated; rolodex: %s", string(b))
	}
}

func TestEditEntryNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeEditFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ", "--label", "x"})
	if exit == 0 {
		t.Fatal("expected error for unknown entry")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestEditRequiresEntryID(t *testing.T) {
	tmp := t.TempDir()
	writeEditFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"--label", "x"})
	if exit == 0 {
		t.Fatal("expected error when entry id is missing")
	}
}

func TestEditWrongPayloadFlagForKind(t *testing.T) {
	tmp := t.TempDir()
	entryID := writeEditFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunEdit(cli.EditOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{entryID, "--pointer-to", "01HB00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected error for kind/flag mismatch")
	}
}
