// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Background git probes must not take git's optional index.lock, or a
// concurrent user `git commit`/`git worktree add` fails with "index.lock
// exists". A stub git on PATH records the env each probe was given.
func TestReadProbesDisableOptionalLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub git")
	}
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec")
	stub := "#!/bin/sh\necho \"x$GIT_OPTIONAL_LOCKS\" >> '" + rec + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	wt := t.TempDir()

	_, _ = git(context.Background(), readTimeout, wt, "status")
	_, _ = HasUncommitted(wt)
	_, _ = diffAgainstNull(wt, "x")
	_, _ = gitPatchIDs(wt, "diff")
	_, _ = worktreeDirty(context.Background(), wt)

	b, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(b))
	if len(lines) < 6 { // patch-ids runs two processes
		t.Fatalf("expected >=6 invocations, got %v", lines)
	}
	for _, l := range lines {
		// Recorded as "x"+value so an unset var is "x", never an empty
		// line that strings.Fields would drop.
		if l != "x0" {
			t.Fatalf("GIT_OPTIONAL_LOCKS=%q, want 0 (all: %v)", strings.TrimPrefix(l, "x"), lines)
		}
	}
}
