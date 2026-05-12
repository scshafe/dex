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

func writeActivateCommandFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{
				"id": "01HB00000000000000000000C1",
				"slug": "echo-it",
				"label": "Echo",
				"kind": "command",
				"command": {
					"template": "echo {msg}",
					"concerns": [{
						"id": "01HB00000000000000000000K1",
						"local_id": "msg",
						"slug": "msg-concern",
						"label": "Message",
						"required": true,
						"strict": false
					}]
				}
			},
			{
				"id": "01HB00000000000000000000C2",
				"slug": "echo-default",
				"label": "Echo default",
				"kind": "command",
				"command": {
					"template": "echo {msg}",
					"concerns": [{
						"id": "01HB00000000000000000000K2",
						"local_id": "msg",
						"slug": "msg-concern",
						"label": "Message",
						"default": "hello",
						"required": true,
						"strict": false
					}]
				}
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActivateCommandDryRunSubstitutesConcerns(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out},
		[]string{"/echo-it", "msg=world"})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out.String())
	}
	if !strings.Contains(out.String(), "echo world") {
		t.Fatalf("expected 'echo world' in stdout; got %q", out.String())
	}
}

func TestActivateCommandDryRunUsesDefault(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out},
		[]string{"/echo-default"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "echo hello") {
		t.Fatalf("expected 'echo hello' (from default); got %q", out.String())
	}
}

func TestActivateCommandMissingRequiredConcern(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out, Stderr: &errBuf},
		[]string{"/echo-it"})
	if exit == 0 {
		t.Fatal("missing required concern should error")
	}
	if !strings.Contains(errBuf.String(), "required") {
		t.Fatalf("expected 'required' in stderr; got %q", errBuf.String())
	}
}

func TestActivateCommandUnknownConcernIgnored(t *testing.T) {
	tmp := t.TempDir()
	writeActivateCommandFixture(t, tmp)
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, DryRun: true, Stdout: &out},
		[]string{"/echo-it", "msg=hi", "bogus=ignored"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "echo hi") {
		t.Fatalf("expected 'echo hi'; got %q", out.String())
	}
}

func TestActivateCommandExecSuccess(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [{
			"id":"01HB00000000000000000000C1","slug":"shell-true","label":"true",
			"kind":"command","command":{"template":"true"}
		}]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/shell-true"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}
}

func TestActivateCommandExecPropagatesExitCode(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [{
			"id":"01HB00000000000000000000C1","slug":"shell-false","label":"false",
			"kind":"command","command":{"template":"false"}
		}]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"/shell-false"})
	if exit == 0 {
		t.Fatal("expected nonzero exit from `false`")
	}
}

func TestActivateCommandExecStdoutCaptured(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [{
			"id":"01HB00000000000000000000C1","slug":"shell-echo","label":"echo",
			"kind":"command","command":{
				"template":"echo {msg}",
				"concerns":[{
					"id":"01HB00000000000000000000K1","local_id":"msg","slug":"msg-concern",
					"label":"msg","required":true,"strict":false
				}]
			}
		}]
	}`
	if err := os.WriteFile(filepath.Join(tmp, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	exit := cli.RunActivate(cli.ActivateOpts{StoreRoot: tmp, Stdout: &out},
		[]string{"/shell-echo", "msg=hello-from-shell"})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out.String(), "hello-from-shell") {
		t.Fatalf("expected exec stdout in our stdout; got %q", out.String())
	}
}
