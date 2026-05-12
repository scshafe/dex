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

func writeAddFixture(t *testing.T, root string) (parentID string) {
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
		"label": "Root",
		"visibility": "bundled",
		"entries": []
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000R1"
}

func TestAddPointerEntry(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{
			"--parent", parent,
			"--slug", "tools",
			"--label", "Tools",
			"--kind", "pointer",
			"--pointer-to", "01HB00000000000000000000T1",
		})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}
	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			Slug    string `json:"slug"`
			Kind    string `json:"kind"`
			Pointer struct {
				To string `json:"to"`
			} `json:"pointer"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Slug != "tools" || e.Kind != "pointer" || e.Pointer.To != "01HB00000000000000000000T1" {
		t.Fatalf("entry not as expected: %+v", e)
	}
}

func TestAddInfoEntryWithContent(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out},
		[]string{
			"--parent", parent,
			"--slug", "readme",
			"--label", "Readme",
			"--kind", "info",
			"--content", "the body text",
		})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	var got struct {
		Entries []struct {
			Slug string `json:"slug"`
			Info struct {
				Content string `json:"content"`
			} `json:"info"`
		} `json:"entries"`
	}
	_ = json.Unmarshal(b, &got)
	if len(got.Entries) != 1 || got.Entries[0].Info.Content != "the body text" {
		t.Fatalf("entry not as expected: %+v", got.Entries)
	}
}

func TestAddFromJSON(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	entryJSON := `{
		"id": "01HB00000000000000000000E1",
		"slug": "broker-status",
		"label": "Broker status",
		"kind": "command",
		"command": {
			"template": "wm broker status --provider {provider}",
			"concerns": [{
				"id": "01HB00000000000000000000K1",
				"local_id": "provider",
				"slug": "provider-concern",
				"label": "Which provider?",
				"required": true,
				"strict": false
			}]
		}
	}`
	entryFile := filepath.Join(tmp, "entry.json")
	if err := os.WriteFile(entryFile, []byte(entryJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"--parent", parent, "--from-json", entryFile})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	b, _ := os.ReadFile(filepath.Join(tmp, "bundled", "root.json"))
	if !strings.Contains(string(b), "broker-status") {
		t.Fatalf("entry not added; rolodex content: %s", string(b))
	}
}

func TestAddRejectsUnknownParent(t *testing.T) {
	tmp := t.TempDir()
	writeAddFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{
			"--parent", "01HB00000000000000000000ZZ",
			"--slug", "tools",
			"--label", "Tools",
			"--kind", "pointer",
			"--pointer-to", "01HB00000000000000000000T1",
		})
	if exit == 0 {
		t.Fatal("expected error for unknown parent")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestAddRejectsDuplicateSlug(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)

	args := []string{
		"--parent", parent,
		"--slug", "tools",
		"--label", "Tools",
		"--kind", "pointer",
		"--pointer-to", "01HB00000000000000000000T1",
	}
	var out, errBuf bytes.Buffer
	if exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, args); exit != 0 {
		t.Fatalf("first add failed: exit=%d stderr=%q", exit, errBuf.String())
	}
	errBuf.Reset()
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, args)
	if exit == 0 {
		t.Fatal("expected duplicate-slug error")
	}
	if !strings.Contains(errBuf.String(), "duplicate") && !strings.Contains(errBuf.String(), "exists") {
		t.Fatalf("expected duplicate hint in stderr; got %q", errBuf.String())
	}
}

func TestAddAutoGeneratesIDIfMissing(t *testing.T) {
	tmp := t.TempDir()
	parent := writeAddFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunAdd(cli.AddOpts{StoreRoot: tmp, Stdout: &out},
		[]string{
			"--parent", parent,
			"--slug", "auto",
			"--label", "Auto",
			"--kind", "info",
			"--content", "x",
		})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "01") {
		t.Fatalf("expected generated ULID in stdout; got %q", out.String())
	}
}
