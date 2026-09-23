//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A junction (mklink /J) needs no privilege, unlike a symlink, which is
// why it is the usual way a Windows user maps a short path onto another
// drive — and the shape that made New Project's Browse… do nothing:
// os.Stat follows it, Wails' os.Lstat check does not.
func TestPickDirectoryDefaultResolvesAJunction(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := exec.Command("cmd", "/c", "mkdir", real).Run(); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(base, "junction")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, real).CombinedOutput()
	if err != nil {
		t.Skipf("cannot create a junction here: %v: %s", err, strings.TrimSpace(string(out)))
	}
	got := pickDirectoryDefault(junction, "")
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want the junction target %q", got, want)
	}
}
