package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLookEditorPath covers the lookup runEditor depends on to turn a
// configured editor name into something exec can start. The dispatch
// test in file_open_test.go stubs runEditorFn wholesale, so without
// this neither lookEditorPath nor its not-found branch — the one that
// produces the "check the editor setting" error a user actually sees —
// is executed by any test on any platform.
func TestLookEditorPath(t *testing.T) {
	name := "hivegui-test-editor"
	if runtime.GOOS == "windows" {
		// exec.LookPath only accepts a PATHEXT extension on Windows.
		name += ".exe"
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit path resolves", func(t *testing.T) {
		got, err := lookEditorPath(bin)
		if err != nil {
			t.Fatalf("lookEditorPath(%q) = _, %v; want no error", bin, err)
		}
		if got != bin {
			t.Fatalf("lookEditorPath(%q) = %q; want %q", bin, got, bin)
		}
	})

	t.Run("explicit path that does not exist", func(t *testing.T) {
		missing := filepath.Join(dir, "absent-"+name)
		if _, err := lookEditorPath(missing); !errors.Is(err, errEditorNotFound) {
			t.Fatalf("lookEditorPath(%q) = _, %v; want errEditorNotFound", missing, err)
		}
	})

	t.Run("bare name not on PATH", func(t *testing.T) {
		// No separator, so this takes the PATH-scan branch on every
		// platform — including the darwin login-shell one.
		const bare = "hivegui-no-such-editor-xyzzy"
		if _, err := lookEditorPath(bare); !errors.Is(err, errEditorNotFound) {
			t.Fatalf("lookEditorPath(%q) = _, %v; want errEditorNotFound", bare, err)
		}
	})
}
