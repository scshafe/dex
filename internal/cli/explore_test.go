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

// writeExploreFixture sets up a store containing one rolodex with three
// entries: a command (with concerns), an info, and a pointer.
func writeExploreFixture(t *testing.T, root string) {
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
				"slug": "broker-status",
				"label": "Broker status",
				"kind": "command",
				"command": {
					"template": "wm broker status --provider {provider}",
					"concerns": [
						{
							"id": "01HB00000000000000000000C1",
							"local_id": "provider",
							"slug": "provider-concern",
							"label": "Which provider?",
							"required": true,
							"strict": false
						}
					]
				},
				"explore": {
					"description": "Snapshot of provider freshness.",
					"examples": [
						{"description": "all", "invocation": "wm broker status"}
					],
					"notes": "non-zero exit if any provider is stale"
				}
			},
			{
				"id": "01HB00000000000000000000F1",
				"slug": "readme",
				"label": "Readme",
				"kind": "info",
				"info": { "content": "hi" }
			},
			{
				"id": "01HB00000000000000000000G1",
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

func TestExploreByULIDCommandKind(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"01HB00000000000000000000E1"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}

	var got struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		Kind     string `json:"kind"`
		Explore  struct {
			Description string `json:"description"`
			Notes       string `json:"notes"`
		} `json:"explore"`
		Concerns []struct {
			LocalID  string `json:"local_id"`
			Required bool   `json:"required"`
		} `json:"concerns"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v raw=%s", err, out.String())
	}
	if got.Slug != "broker-status" {
		t.Fatalf("slug: got %q", got.Slug)
	}
	if got.Kind != "command" {
		t.Fatalf("kind: got %q", got.Kind)
	}
	if got.Explore.Description == "" {
		t.Fatal("explore.description missing")
	}
	if len(got.Concerns) != 1 || got.Concerns[0].LocalID != "provider" {
		t.Fatalf("concerns: %+v", got.Concerns)
	}
}

func TestExploreByPathInfoKind(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)

	var out bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/readme"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got struct {
		Slug     string                   `json:"slug"`
		Kind     string                   `json:"kind"`
		Concerns []map[string]interface{} `json:"concerns"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Slug != "readme" || got.Kind != "info" {
		t.Fatalf("got %+v", got)
	}
	if len(got.Concerns) != 0 {
		t.Fatalf("non-command should have no concerns; got %+v", got.Concerns)
	}
}

func TestExploreByPathPointerKind(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out},
		[]string{"/tools"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "tools") {
		t.Fatalf("human output should mention 'tools'; got %q", out.String())
	}
}

func TestExploreNoArg(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("explore without arg should error")
	}
	if !strings.Contains(errBuf.String(), "argument") {
		t.Fatalf("expected 'argument' in stderr; got %q", errBuf.String())
	}
}

func TestExploreNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeExploreFixture(t, tmp)
	var out, errBuf bytes.Buffer
	// Use a valid 26-char ULID that doesn't exist in the fixture.
	exit := cli.RunExplore(cli.ExploreOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected non-zero for unknown ULID")
	}
}
