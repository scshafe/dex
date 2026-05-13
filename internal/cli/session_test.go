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
