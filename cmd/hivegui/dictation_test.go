// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"path/filepath"
	"testing"
)

// Spec 575: the dictation box redials for the life of a window, so its
// dial must never start a daemon.
func TestDictationDialNeverSpawns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	t.Setenv("HIVE_SOCKET", filepath.Join(dir, "none.sock"))
	before := spawnedHived.Load()
	a := &App{}
	if _, err := a.dictationDial(); err == nil {
		t.Fatal("dialled a daemon that is not running")
	}
	if spawnedHived.Load() != before {
		t.Fatal("the dictation dial spawned hived")
	}
}
