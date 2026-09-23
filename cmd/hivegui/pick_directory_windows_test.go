//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/proc"
)

// A junction (mklink /J) needs no privilege, unlike a symlink, which is
// why it is the usual way a Windows user maps a short path onto another
// drive — and the shape that made New Project's Browse… do nothing:
// os.Stat follows it, Wails' os.Lstat check does not.
func TestPickDirectoryDefaultResolvesAJunction(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(base, "junction")
	// mklink is a cmd built-in; proc.Command keeps it consoleless.
	out, err := proc.Command("cmd", "/c", "mklink", "/J", junction, real).CombinedOutput()
	if err != nil {
		t.Skipf("cannot create a junction here: %v: %s", err, strings.TrimSpace(string(out)))
	}
	got := pickDirectoryDefault(junction, "")
	want, err := resolveDir(real)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want the junction target %q", got, want)
	}
}
