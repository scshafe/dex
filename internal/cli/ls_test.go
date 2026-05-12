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

func writeFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rolodex := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Bundled root",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000E1",
				"slug": "tools",
				"label": "Tools",
				"kind": "pointer",
				"pointer": { "to": "01HB00000000000000000000T1" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLsMergedRootJSON(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{
		StoreRoot: tmp,
		JSON:      true,
		Stdout:    &out,
		Stderr:    &errBuf,
	}, nil)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	var got []struct {
		Slug  string `json:"slug"`
		Label string `json:"label"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, out.String())
	}
	if len(got) != 1 || got[0].Slug != "tools" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsMergedRootEmpty(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out}, nil)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("expected [], got %q", out.String())
	}
}

func TestLsHumanReadable(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out}, nil)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "tools") {
		t.Fatalf("expected human output to mention 'tools', got: %s", out.String())
	}
}
