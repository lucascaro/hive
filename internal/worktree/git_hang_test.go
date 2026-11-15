//go:build !windows

package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeGit puts a `git` on PATH that forks a grandchild holding the
// inherited stdout/stderr, then hangs: the shape of `git fetch` with a
// stuck ssh child. Killing only the leader leaves the pipe open, so a
// CombinedOutput caller blocks until the grandchild exits. `remote
// get-url` answers at once so PrepareBase reaches its fetch. The
// grandchild's pid lands in the returned file.
func fakeGit(t *testing.T) (pidFile string) {
	t.Helper()
	dir := t.TempDir()
	pidFile = filepath.Join(dir, "grandchild.pid")
	t.Setenv("FAKE_GIT_PIDFILE", pidFile)
	script := "#!/bin/sh\n" +
		"case \"$*\" in *get-url*) exit 0;; esac\n" +
		"sleep 20 &\n" +
		"echo $! > \"$FAKE_GIT_PIDFILE\"\n" +
		"sleep 20\n"
	writeFakeGit(t, dir, script)
	return pidFile
}

func writeFakeGit(t *testing.T, dir, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// requireDescendantGone fails unless the grandchild fakeGit forked was
// killed. A fast return alone proves nothing: WaitDelay also unblocks a
// caller while a leaked descendant lives on, so this is what shows the
// whole process tree was killed, not just the leader.
func requireDescendantGone(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("fake git never forked its grandchild: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("grandchild pid %q: %v", raw, err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived the timeout kill", pid)
		}
	}
}

// A timed-out git must return promptly even when a grandchild holds its
// pipe. Held under gitMu this is what froze every close at "Removing
// worktree".
func TestGitTimeoutReapsGrandchildHoldingPipe(t *testing.T) {
	pidFile := fakeGit(t)
	start := time.Now()
	_, err := git(context.Background(), time.Second, t.TempDir(), "status")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("git returned after %s; a grandchild held the pipe open", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	requireDescendantGone(t, pidFile)
}

func TestGitWorktreeAddTimeoutReapsGrandchildHoldingPipe(t *testing.T) {
	pidFile := fakeGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, _ = gitWorktreeAdd(ctx, t.TempDir(), "x")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gitWorktreeAdd returned after %s", elapsed)
	}
	requireDescendantGone(t, pidFile)
}

func TestPrepareBaseFetchTimeoutReapsGrandchildHoldingPipe(t *testing.T) {
	pidFile := fakeGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, _ = PrepareBase(ctx, t.TempDir())
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("PrepareBase returned after %s", elapsed)
	}
	requireDescendantGone(t, pidFile)
}

// A git that succeeds but leaves a background helper holding the pipe,
// as `git fetch` can when it auto-starts maintenance. WaitDelay then
// lapses on a command that worked.
func fakeGitExitsOKLeavingHelper(t *testing.T) {
	t.Helper()
	writeFakeGit(t, t.TempDir(), "#!/bin/sh\n"+
		"case \"$*\" in *get-url*) exit 0;; esac\n"+
		"case \"$*\" in *fetch*|*worktree*add*|*status*) sleep 6 & exit 0;; esac\n"+
		"exit 0\n")
}

// ErrWaitDelay after a clean exit is a success, not a failed fetch: a
// stale-base dialog, or "create failed" for a worktree git did create,
// would be wrong.
func TestCleanExitWithLingeringHelperIsNotAFailure(t *testing.T) {
	fakeGitExitsOKLeavingHelper(t)
	if _, err := PrepareBase(context.Background(), t.TempDir()); err != nil {
		t.Errorf("PrepareBase: %v, want success", err)
	}
	if _, err := gitWorktreeAdd(context.Background(), t.TempDir(), "x"); err != nil {
		t.Errorf("gitWorktreeAdd: %v, want success", err)
	}
	if _, err := git(context.Background(), 10*time.Second, t.TempDir(), "status"); err != nil {
		t.Errorf("git: %v, want success", err)
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

// A deletion that outlives the bound must say so, and hand back a
// channel that closes when it does finish — the caller needs both to
// prune the entry afterwards.
func TestRemoveAllBoundedReportsPendingThenFinishes(t *testing.T) {
	release := make(chan struct{})
	old := removeAll
	removeAll = func(string) error { <-release; return nil }
	t.Cleanup(func() { removeAll = old })

	err := removeAllBounded("/x", 50*time.Millisecond)
	var pending *RemovalPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("err = %v, want *RemovalPendingError", err)
	}
	select {
	case <-pending.Done:
		t.Fatal("Done closed before the deletion finished")
	default:
	}
	close(release)
	select {
	case <-pending.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("Done never closed after the deletion finished")
	}
}
