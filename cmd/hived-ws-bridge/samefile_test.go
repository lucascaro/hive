// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A symlink to the real socket or state dir is the same file under
// another name; requireIsolation catches it via sameFile.
func TestSameFileSeesThroughSymlinks(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !sameFile(link, target) {
		t.Error("symlink not recognised as the same file")
	}
	if sameFile(tmp, target) || sameFile(filepath.Join(tmp, "missing"), target) {
		t.Error("distinct or missing path reported as the same file")
	}
}
