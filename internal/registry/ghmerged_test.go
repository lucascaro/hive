package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// No registry test may reach the network: gh is stubbed out for the
// whole package, and the cases that care install their own answer.
func TestMain(m *testing.M) {
	ghMergedLookup = func(string) map[string]bool { return nil }
	os.Exit(m.Run())
}

// stubGHMerged makes the GitHub lookup report the given branch names
// as merged, restoring the package default afterwards.
func stubGHMerged(t *testing.T, names ...string) {
	t.Helper()
	prev := ghMergedLookup
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	ghMergedLookup = func(string) map[string]bool { return set }
	t.Cleanup(func() { ghMergedLookup = prev })
}

func TestListWorktrees_GHMergedOverlayMarksRows(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "shipped")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	runGit(t, p.Cwd, "branch", "shipped-orphan")

	stubGHMerged(t, "shipped", "shipped-orphan")

	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	row := findWT(resp, wt)
	if row == nil {
		t.Fatalf("worktree row missing: %+v", resp.Worktrees)
	}
	if !row.Merged {
		t.Errorf("worktree Merged = false, want true (%+v)", row)
	}
	orphan := findOrphan(resp, "shipped-orphan")
	if orphan == nil {
		t.Fatalf("orphan missing: %+v", resp.OrphanBranches)
	}
	if !orphan.Merged {
		t.Errorf("orphan Merged = false, want true")
	}
}

// The daemon's refusal must agree with what the client rendered: a
// merged branch's commits are upstream, so removing the worktree
// needs no force.
func TestRemoveWorktree_AllowsMergedBranchWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "shipped")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")

	// Without the overlay this is the ErrWorktreeUnpushed case.
	if err := r.RemoveWorktree(p.ID, wt, false, false, false); !errors.Is(err, ErrWorktreeUnpushed) {
		t.Fatalf("precondition: got %v, want ErrWorktreeUnpushed", err)
	}
	stubGHMerged(t, "shipped")
	if err := r.RemoveWorktree(p.ID, wt, false, false, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, serr := os.Stat(wt); !os.IsNotExist(serr) {
		t.Fatalf("worktree still present: %v", serr)
	}
}

func TestDeleteBranch_AllowsGHMergedWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "shipped")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	// The branch must be orphaned before it can be deleted.
	runGit(t, p.Cwd, "worktree", "remove", "--force", wt)

	if err := r.DeleteBranch(p.ID, "shipped", false, false); !errors.Is(err, ErrBranchUnmerged) {
		t.Fatalf("precondition: got %v, want ErrBranchUnmerged", err)
	}
	stubGHMerged(t, "shipped")
	if err := r.DeleteBranch(p.ID, "shipped", false, false); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
}
