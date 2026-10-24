//go:build !windows

package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGit puts a `git` on PATH that forks a grandchild holding the
// inherited stdout/stderr, then hangs: the shape of `git fetch` with a
// stuck ssh child. Killing only the leader leaves the pipe open, so a
// CombinedOutput caller blocks until the grandchild exits.
func fakeGit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *get-url*) exit 0;; esac\nsleep 20 &\nsleep 20\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A timed-out git must return promptly even when a grandchild holds its
// pipe. Held under gitMu this is what froze every close at "Removing
// worktree".
func TestGitTimeoutReapsGrandchildHoldingPipe(t *testing.T) {
	fakeGit(t)
	start := time.Now()
	_, err := git(context.Background(), 200*time.Millisecond, t.TempDir(), "status")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("git returned after %s; a grandchild held the pipe open", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
}

func TestGitWorktreeAddTimeoutReapsGrandchildHoldingPipe(t *testing.T) {
	fakeGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = gitWorktreeAdd(ctx, t.TempDir(), "x")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gitWorktreeAdd returned after %s", elapsed)
	}
}

func TestPrepareBaseFetchTimeoutReapsGrandchildHoldingPipe(t *testing.T) {
	fakeGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = PrepareBase(ctx, t.TempDir())
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("PrepareBase returned after %s", elapsed)
	}
}

func TestRemoveAllBoundedRemoves(t *testing.T) {
	dir := t.TempDir()
	if err := removeAllBounded(dir, time.Second); err != nil {
		t.Fatalf("removeAllBounded: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir survived: %v", err)
	}
}
