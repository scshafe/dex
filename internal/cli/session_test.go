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

// writeMinimalStore sets up a minimal store dir tree so cli.Run* calls
// that pass StoreRoot don't fail on a missing tier dir.
func writeMinimalStore(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		if err := os.MkdirAll(filepath.Join(tmp, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return tmp
}

func TestSessionStartPrintsID(t *testing.T) {
	store := writeMinimalStore(t)
	sessDir := t.TempDir()
	var out bytes.Buffer
	exit := cli.RunSessionStart(cli.SessionOpts{
		StoreRoot:  store,
		SessionDir: sessDir,
		Stdout:     &out,
	})
	if exit != 0 {
		t.Fatalf("exit: %d, stdout=%s", exit, out.String())
	}
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v raw=%s", err, out.String())
	}
	if !strings.HasPrefix(payload.SessionID, "ses_") {
		t.Fatalf("session_id should have ses_ prefix; got %q", payload.SessionID)
	}
	// Verify the file actually got created in sessDir.
	entries, _ := os.ReadDir(sessDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in session dir, got %d", len(entries))
	}
}

func TestSessionStepDrillSucceeds(t *testing.T) {
	store := writeMinimalStore(t)
	// Populate the store with a minimal rolodex containing one entry.
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"readme","label":"Readme","kind":"info","info":{"content":"hi"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(store, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}

	sessDir := t.TempDir()

	// First, start a session.
	var startOut bytes.Buffer
	if exit := cli.RunSessionStart(cli.SessionOpts{
		StoreRoot: store, SessionDir: sessDir, Stdout: &startOut,
	}); exit != 0 {
		t.Fatalf("start: exit=%d out=%s", exit, startOut.String())
	}
	var startPayload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(startOut.Bytes(), &startPayload); err != nil {
		t.Fatalf("decode start: %v", err)
	}

	// Then, step with a drill action.
	action := strings.NewReader(`{"action":"drill","target":"/readme"}`)
	var stepOut, stepErr bytes.Buffer
	exit := cli.RunSessionStep(cli.SessionOpts{
		StoreRoot:  store,
		SessionDir: sessDir,
		Stdin:      action,
		Stdout:     &stepOut,
		Stderr:     &stepErr,
	}, []string{startPayload.SessionID})
	if exit != 0 {
		t.Fatalf("step: exit=%d stdout=%s stderr=%s", exit, stepOut.String(), stepErr.String())
	}

	var env struct {
		OK      bool `json:"ok"`
		Session struct {
			Cursor struct {
				EntryID string `json:"entry_id"`
				Mode    string `json:"mode"`
			} `json:"cursor"`
		} `json:"session"`
	}
	if err := json.Unmarshal(stepOut.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v raw=%s", err, stepOut.String())
	}
	if !env.OK {
		t.Fatalf("envelope ok should be true; raw=%s", stepOut.String())
	}
	if env.Session.Cursor.EntryID != "01HB00000000000000000000E1" {
		t.Fatalf("cursor.entry_id: got %q want %q", env.Session.Cursor.EntryID, "01HB00000000000000000000E1")
	}
	if env.Session.Cursor.Mode != "entry" {
		t.Fatalf("cursor.mode: got %q want entry", env.Session.Cursor.Mode)
	}
}

func TestSessionStepUnknownAction(t *testing.T) {
	store := writeMinimalStore(t)
	r := `{"schema_version":1,"id":"01HB00000000000000000000R1","slug":"root","label":"Root","visibility":"bundled","entries":[]}`
	if err := os.WriteFile(filepath.Join(store, "bundled", "root.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	sessDir := t.TempDir()
	var startOut bytes.Buffer
	cli.RunSessionStart(cli.SessionOpts{StoreRoot: store, SessionDir: sessDir, Stdout: &startOut})
	var sp struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(startOut.Bytes(), &sp)

	var out, errBuf bytes.Buffer
	exit := cli.RunSessionStep(cli.SessionOpts{
		StoreRoot:  store,
		SessionDir: sessDir,
		Stdin:      strings.NewReader(`{"action":"floop"}`),
		Stdout:     &out, Stderr: &errBuf,
	}, []string{sp.SessionID})
	// Unknown action is a protocol-level error from Apply; exit 1.
	if exit != 1 {
		t.Fatalf("expected exit 1 for unknown action; got %d, stdout=%s, stderr=%s",
			exit, out.String(), errBuf.String())
	}
}
