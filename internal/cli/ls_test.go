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

func TestLsByID(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"01HB00000000000000000000R1"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, out.String())
	}
	if len(got) != 1 || got[0].Slug != "tools" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsByIDNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HZ00000000000000000000ZZ"})
	if exit == 0 {
		t.Fatal("expected non-zero exit for not-found")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr; got %q", errBuf.String())
	}
}

func TestLsByPathRoot(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp) // bundled root with /tools (pointer to T1)
	// Add the target rolodex so the pointer resolves.
	target := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000T1",
		"slug": "tools-collection",
		"label": "Tools collection",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000C1",
				"slug": "hammer",
				"label": "Hammer",
				"kind": "info",
				"info": { "content": "the hammer" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "tools.json"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/tools"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "hammer" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsByPathSlashListsRoot(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "tools" {
		t.Fatalf("got %+v", got)
	}
}

func TestLsByPathNonPointerErrors(t *testing.T) {
	tmp := t.TempDir()
	// Bundled root with /readme (info, not pointer).
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
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
				"slug": "readme",
				"label": "Readme",
				"kind": "info",
				"info": { "content": "hi" }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(rolodex), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/readme"})
	if exit == 0 {
		t.Fatalf("expected non-zero exit for ls on info entry")
	}
	if !strings.Contains(errBuf.String(), "explore") {
		t.Fatalf("expected stderr to suggest 'explore', got %q", errBuf.String())
	}
}

func TestLsByPathNotFound(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunLs(cli.LsOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/does-not-exist"})
	if exit == 0 {
		t.Fatalf("expected non-zero exit for unknown path")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("expected 'not found' in stderr, got %q", errBuf.String())
	}
}
