package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scshafe/dex/internal/cli"
)

func writeDoctorCleanFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	r1 := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"tools","label":"Tools","kind":"pointer","pointer":{"to":"01HB00000000000000000000T1"}}
		]
	}`
	r2 := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000T1",
		"slug": "tools-collection",
		"label": "Tools",
		"visibility": "bundled",
		"entries": []
	}`
	_ = os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(r1), 0o644)
	_ = os.WriteFile(filepath.Join(root, "bundled", "tools.json"), []byte(r2), 0o644)
}

func writeDoctorDanglingFixture(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	r := `{
		"schema_version": 1,
		"id": "01HB00000000000000000000R1",
		"slug": "root",
		"label": "Root",
		"visibility": "bundled",
		"entries": [
			{"id":"01HB00000000000000000000E1","slug":"orphan","label":"Orphan","kind":"pointer","pointer":{"to":"01HB00000000000000000000ZZ"}}
		]
	}`
	_ = os.WriteFile(filepath.Join(root, "bundled", "root.json"), []byte(r), 0o644)
}

func TestDoctorCleanStore(t *testing.T) {
	tmp := t.TempDir()
	writeDoctorCleanFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunDoctor(cli.DoctorOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit != 0 {
		t.Fatalf("clean store should exit 0; exit=%d stderr=%q", exit, errBuf.String())
	}
	if !strings.Contains(out.String(), "clean") && !strings.Contains(out.String(), "OK") && !strings.Contains(out.String(), "no issues") {
		t.Fatalf("expected positive output for clean store; got %q", out.String())
	}
}

func TestDoctorDanglingPointer(t *testing.T) {
	tmp := t.TempDir()
	writeDoctorDanglingFixture(t, tmp)
	var out, errBuf bytes.Buffer
	exit := cli.RunDoctor(cli.DoctorOpts{StoreRoot: tmp, Stdout: &out, Stderr: &errBuf}, nil)
	if exit == 0 {
		t.Fatal("dangling pointer should produce non-zero exit")
	}
	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "dangling") && !strings.Contains(combined, "ZZ") {
		t.Fatalf("expected dangling-pointer mention in output; got out=%q stderr=%q", out.String(), errBuf.String())
	}
}

func TestDoctorEmptyStore(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"bundled", "personal", "private", "ephemeral"} {
		_ = os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	var out bytes.Buffer
	exit := cli.RunDoctor(cli.DoctorOpts{StoreRoot: tmp, Stdout: &out}, nil)
	if exit != 0 {
		t.Fatalf("empty store should exit 0; exit=%d", exit)
	}
}
