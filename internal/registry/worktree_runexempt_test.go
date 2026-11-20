// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// B3-R7 (spec 529, B-25): RemoveWorktreeForRun skips ONLY the unpushed
// refusal, for a kept branch under hive/wf/<run>/.

// committedWorktree is a worktree on branch holding one unpushed commit
// and a clean tree, so only the unpushed check stands in the way.
func committedWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := newWorktree(t, repo, branch)
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	return wt
}

func branchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	return gitOutput(t, repo, "branch", "--list", branch) != ""
}

func TestRemoveWorktree_RunBranchUnpushedExemptForPlugin(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := committedWorktree(t, p.Cwd, "hive/wf/run-1/x")

	// Without the run id the refusal stands.
	if err := r.RemoveWorktree(p.ID, wt, false, false, false); !errors.Is(err, ErrWorktreeUnpushed) {
		t.Fatalf("no run id: got %v, want ErrWorktreeUnpushed", err)
	}
	if err := r.RemoveWorktreeForRun(p.ID, wt, false, false, false, "run-1"); err != nil {
		t.Fatalf("exempt remove: %v", err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("worktree still on disk")
	}
	if !branchExists(t, p.Cwd, "hive/wf/run-1/x") {
		t.Error("the run branch was deleted; it must keep every commit")
	}
}

func TestRemoveWorktree_ExemptionStillRefusesDirty(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := committedWorktree(t, p.Cwd, "hive/wf/run-1/x")
	mustWriteFile(t, filepath.Join(wt, "scratch.txt"), "unsaved")
	if err := r.RemoveWorktreeForRun(p.ID, wt, false, false, false, "run-1"); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("got %v, want ErrWorktreeDirty", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("dirty worktree removed: %v", err)
	}
}

func TestRemoveWorktree_ExemptionNotForOtherRunOrPrefix(t *testing.T) {
	skipNonPosix(t)
	cases := []struct{ name, branch, run string }{
		{"other run", "hive/wf/run-2/x", "run-1"},
		{"run id is a prefix of the branch's run", "hive/wf/run-10/x", "run-1"},
		{"outside hive/wf", "feature/x", "run-1"},
		{"hive/ but not wf", "hive/run-1/x", "run-1"},
		{"run segment only, no slash", "hive/wf/run-1", "run-1"},
		{"invalid run id: traversal", "hive/wf/x/y", ".."},
		{"invalid run id: slash", "hive/wf/run-1/x", "run-1/x"},
		{"invalid run id: trailing slash", "hive/wf/run-1/x", "run-1/"},
		{"invalid run id: uppercase", "hive/wf/RUN/x", "RUN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, p := freshRegistryWithProject(t)
			wt := committedWorktree(t, p.Cwd, c.branch)
			err := r.RemoveWorktreeForRun(p.ID, wt, false, false, false, c.run)
			if !errors.Is(err, ErrWorktreeUnpushed) {
				t.Errorf("got %v, want ErrWorktreeUnpushed", err)
			}
			if _, serr := os.Stat(wt); serr != nil {
				t.Errorf("worktree removed: %v", serr)
			}
		})
	}
}

func TestRemoveWorktree_ExemptionNotWithDeleteBranch(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := committedWorktree(t, p.Cwd, "hive/wf/run-1/x")
	err := r.RemoveWorktreeForRun(p.ID, wt, false, true, false, "run-1")
	if !errors.Is(err, ErrWorktreeUnpushed) {
		t.Fatalf("got %v, want ErrWorktreeUnpushed", err)
	}
	if _, serr := os.Stat(wt); serr != nil {
		t.Errorf("worktree removed: %v", serr)
	}
	if !branchExists(t, p.Cwd, "hive/wf/run-1/x") {
		t.Error("branch deleted")
	}
}

// runBranchSession is a session sitting in a committed hive/wf/run-1/x
// worktree, so every exemption input holds and only the claim differs.
func runBranchSession(t *testing.T, r *Registry, p *Project) (*Entry, string) {
	t.Helper()
	wt := committedWorktree(t, p.Cwd, "hive/wf/run-1/x")
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", WorktreePath: wt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return e, wt
}

func TestRemoveWorktree_ExemptionStillRefusesInUse(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, wt := runBranchSession(t, r, p)
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)
	for _, force := range []bool{false, true} {
		err := r.RemoveWorktreeForRun(p.ID, wt, force, false, false, "run-1")
		if !errors.Is(err, ErrWorktreeInUse) {
			t.Fatalf("force=%v: got %v, want ErrWorktreeInUse", force, err)
		}
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed from under a live session: %v", err)
	}
}

func TestRemoveWorktree_ArchivedHolderStillInUse(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, wt := runBranchSession(t, r, p)
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		err := r.RemoveWorktreeForRun(p.ID, wt, force, false, false, "run-1")
		if !errors.Is(err, ErrWorktreeInUse) {
			t.Fatalf("force=%v: got %v, want ErrWorktreeInUse", force, err)
		}
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed from under an archived session: %v", err)
	}
}
