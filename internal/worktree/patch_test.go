// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// DumpPatch must capture every kind of uncommitted work, skip a file
// that is only stat-dirty, and — being a read — leave the index file
// alone (diff-index never refreshes it; porcelain `git diff` does).
func TestDumpPatchCasesAndNoIndexWrite(t *testing.T) {
	dir := initRepo(t)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mod.txt", "a\nb\n")
	write("staged.txt", "x\n")
	write("touch.txt", "same\n")
	write("del.txt", "del\n")
	write("bin.dat", "\x00\x01\x02")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "base")

	write("mod.txt", "a\nB\n")
	write("staged.txt", "x\ny\n")
	mustGit(t, dir, "add", "staged.txt")
	write("bin.dat", "\x00\x01\x09\xff")
	write("untracked.txt", "new\n")
	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	// Touched but unchanged: stat-dirty only, must not appear.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "touch.txt"), future, future); err != nil {
		t.Fatal(err)
	}

	idx := filepath.Join(dir, ".git", "index")
	before, err := os.Stat(idx)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "p.patch")
	if err := DumpPatch(dir, out, 1<<20); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("DumpPatch rewrote the index: %v/%d -> %v/%d",
			before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^diff --git a/(\S+) b/`).FindAllStringSubmatch(string(b), -1) {
		got[m[1]] = true
	}
	for _, want := range []string{"mod.txt", "staged.txt", "del.txt", "bin.dat", "untracked.txt"} {
		if !got[want] {
			t.Errorf("patch missing %s; have %v", want, got)
		}
	}
	if got["touch.txt"] {
		t.Error("touched-but-unchanged file must not be in the patch")
	}
	if !regexp.MustCompile(`GIT binary patch`).Match(b) {
		t.Error("binary change not captured as a binary patch")
	}
}
