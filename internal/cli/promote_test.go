package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writePromoteFixture(t *testing.T, root string) (rolodexID string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "scratch",
		"label": "Scratch",
		"visibility": "ephemeral",
		"entries": []
	}`
	if err := os.WriteFile(filepath.Join(root, "ephemeral", "scratch.json"), []byte(r), 0o644); err != nil {
		t.Fatal(err)
	}
	return "01HB00000000000000000000R1"
}

func TestPromoteToPersonal(t *testing.T) {
	tmp := t.TempDir()
	rolodexID := writePromoteFixture(t, tmp)

	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{rolodexID, "--to", "personal"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errBuf.String())
	}

	if _, err := os.Stat(filepath.Join(tmp, "ephemeral", "scratch.json")); !os.IsNotExist(err) {
		t.Fatal("source file still exists after promote")
	}
	personalFiles, _ := filepath.Glob(filepath.Join(tmp, "personal", "*.json"))
	if len(personalFiles) != 1 {
		t.Fatalf("expected 1 file in personal/, got %v", personalFiles)
	}
	b, _ := os.ReadFile(personalFiles[0])
	if !strings.Contains(string(b), `"visibility": "personal"`) {
		t.Fatalf("visibility not rewritten in moved file: %s", string(b))
	}
}

func TestPromoteRolodexNotFound(t *testing.T) {
	tmp := t.TempDir()
	writePromoteFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{"01HB00000000000000000000ZZ", "--to", "personal"})
	if exit == 0 {
		t.Fatal("expected error for unknown rolodex")
	}
}

func TestPromoteInvalidTier(t *testing.T) {
	tmp := t.TempDir()
	rolodexID := writePromoteFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf},
		[]string{rolodexID, "--to", "nonsense"})
	if exit == 0 {
		t.Fatal("expected error for invalid tier")
	}
}

func TestPromoteRequiresArgs(t *testing.T) {
	tmp := t.TempDir()
	writePromoteFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunPromote(cli.PromoteOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("expected error when args are missing")
	}
}
