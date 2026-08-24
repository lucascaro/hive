package registry

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// No registry test may reach the network: gh is stubbed out for the
// whole package, and the cases that care install their own answer.
func TestMain(m *testing.M) {
	ghMergedLookup = func(string) ghMerged { return nil }
	os.Exit(m.Run())
}

// stubGHMerged makes the GitHub lookup report each branch as merged at
// the commit its local ref currently points to — the shape of a PR
// merged with no further work on the branch. Restored afterwards.
func stubGHMerged(t *testing.T, repo string, names ...string) {
	t.Helper()
	set := ghMerged{}
	for _, n := range names {
		set[n] = revParse(t, repo, "refs/heads/"+n)
	}
	stubGHMergedAt(t, set)
}

// stubGHMergedAt is the explicit form: branch to merged commit, for the
// cases where the two must differ.
func stubGHMergedAt(t *testing.T, set ghMerged) {
	t.Helper()
	prev := ghMergedLookup
	ghMergedLookup = func(string) ghMerged { return set }
	t.Cleanup(func() { ghMergedLookup = prev })
}

// revParse resolves a ref in repo to its sha.
func revParse(t *testing.T, repo, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestListWorktrees_GHMergedOverlayMarksRows(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "shipped")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	runGit(t, p.Cwd, "branch", "shipped-orphan")

	stubGHMerged(t, p.Cwd, "shipped", "shipped-orphan")

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
	stubGHMerged(t, p.Cwd, "shipped")
	if err := r.RemoveWorktree(p.ID, wt, false, false, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, serr := os.Stat(wt); !os.IsNotExist(serr) {
		t.Fatalf("worktree still present: %v", serr)
	}
}

// The dangerous shape: GitHub says the branch was merged, but the user
// has committed on it since. A name-only match would clear the unpushed
// refusal and then -D the ref, destroying those commits.
func TestRemoveWorktree_GHMergedDoesNotCoverLaterCommits(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "shipped")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "merged work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	mergedAt := revParse(t, p.Cwd, "refs/heads/shipped")

	// Work that landed after the PR merged.
	mustWriteFile(t, filepath.Join(wt, "b.txt"), "later work")
	runGit(t, wt, "add", "b.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "b")

	stubGHMergedAt(t, ghMerged{"shipped": mergedAt})

	if err := r.RemoveWorktree(p.ID, wt, false, false, false); !errors.Is(err, ErrWorktreeUnpushed) {
		t.Fatalf("got %v, want ErrWorktreeUnpushed", err)
	}
	if _, serr := os.Stat(wt); serr != nil {
		t.Fatalf("worktree removed despite later commits: %v", serr)
	}
}

// A branch whose name matches a merged PR but whose history has nothing
// to do with it (a reused name) must not read as merged either.
func TestDeleteBranch_GHMergedIgnoresUnrelatedHistory(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	runGit(t, p.Cwd, "branch", "recycled")
	wt := newWorktree(t, p.Cwd, "other")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "unrelated")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")
	unrelated := revParse(t, p.Cwd, "refs/heads/other")

	// GitHub claims "recycled" was merged — at a commit that is not in
	// its history at all.
	runGit(t, p.Cwd, "checkout", "-q", "recycled")
	mustWriteFile(t, filepath.Join(p.Cwd, "c.txt"), "wip")
	runGit(t, p.Cwd, "add", "c.txt")
	runGit(t, p.Cwd, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "c")
	runGit(t, p.Cwd, "checkout", "-q", "-")
	stubGHMergedAt(t, ghMerged{"recycled": unrelated})

	if err := r.DeleteBranch(p.ID, "recycled", false, false); !errors.Is(err, ErrBranchUnmerged) {
		t.Fatalf("got %v, want ErrBranchUnmerged", err)
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
	stubGHMerged(t, p.Cwd, "shipped")
	if err := r.DeleteBranch(p.ID, "shipped", false, false); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
}
