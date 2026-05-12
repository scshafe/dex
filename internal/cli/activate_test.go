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

func writeActivateFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rootR := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000P1","slug":"tools","label":"Tools","kind":"pointer","pointer":{"to":"01HB00000000000000000000T1"}},
			{"id":"01HB00000000000000000000E2","slug":"readme","label":"Readme","kind":"info","info":{"content":"the readme body"}},
			{"id":"01HB00000000000000000000E3","slug":"dynamic","label":"Dynamic","kind":"info","info":{"provider":"some-provider"}}
		]
	}`
	target := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000T1",
		"slug": "tools-collection",
		"label": "Tools collection",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000H1","slug":"hammer","label":"Hammer","kind":"info","info":{"content":"a hammer"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(rootR), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bundled", "tools.json"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActivatePointerDrillsIn(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, JSON: true, Stdout: &out},
		[]string{"/tools"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	var got []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v raw=%s", err, out.String())
	}
	if len(got) != 1 || got[0].Slug != "hammer" {
		t.Fatalf("got %+v", got)
	}
}

func TestActivateInfoContentPrintsContent(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out},
		[]string{"/readme"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "the readme body") {
		t.Fatalf("expected content in stdout; got %q", out.String())
	}
}

func TestActivateInfoProviderUnsupported(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/dynamic"})
	if exit == 0 {
		t.Fatal("info-provider should error in v1")
	}
	if !strings.Contains(errBuf.String(), "provider") {
		t.Fatalf("expected 'provider' in stderr; got %q", errBuf.String())
	}
}

func TestActivateRequiresArg(t *testing.T) {
	tmp := t.TempDir()
	writeActivateFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("activate without arg should error")
	}
}
