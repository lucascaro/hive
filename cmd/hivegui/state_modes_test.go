// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// When the GUI is first to create the state dir it is private; one that
// already exists is left exactly as the user (or the daemon) made it.
func TestStateWritersCreatePrivateDirAndLeaveExistingAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix mode bits on Windows")
	}
	writers := map[string]func() error{
		"state json":      func() error { return writeStateJSON("x.json", map[string]int{"a": 1}) },
		"update settings": func() error { return saveUpdateSettings(UpdateSettings{}) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			fresh := filepath.Join(t.TempDir(), "state")
			t.Setenv("HIVE_STATE_DIR", fresh)
			if err := write(); err != nil {
				t.Fatal(err)
			}
			if fi, err := os.Stat(fresh); err != nil || fi.Mode().Perm() != 0o700 {
				t.Errorf("new state dir: %v mode %v, want 700", err, fi.Mode().Perm())
			}
			existing := t.TempDir()
			if err := os.Chmod(existing, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HIVE_STATE_DIR", existing)
			if err := write(); err != nil {
				t.Fatal(err)
			}
			if fi, _ := os.Stat(existing); fi.Mode().Perm() != 0o755 {
				t.Errorf("existing dir was re-chmodded to %v", fi.Mode().Perm())
			}
			entries, _ := os.ReadDir(existing)
			for _, e := range entries {
				if fi, _ := e.Info(); fi.Mode().Perm() != 0o600 {
					t.Errorf("%s mode %v, want 600", e.Name(), fi.Mode().Perm())
				}
			}
		})
	}
}
