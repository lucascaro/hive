package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// findWT returns the inventory row for a path, or nil.
func findWT(resp wire.WorktreesResp, path string) *wire.WorktreeInfo {
	want := worktree.ResolvePath(path)
	for i := range resp.Worktrees {
		if resp.Worktrees[i].Path == want {
			return &resp.Worktrees[i]
		}
	}
	return nil
}

func findOrphan(resp wire.WorktreesResp, name string) *wire.BranchInfo {
	for i := range resp.OrphanBranches {
		if resp.OrphanBranches[i].Name == name {
			return &resp.OrphanBranches[i]
		}
	}
	return nil
}

// newWorktree creates a worktree on a fresh branch directly via git,
// bypassing session creation — most of these tests care about the
// worktree, not about a PTY.
func newWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	path := worktree.WorktreePath(repo, branch)
	runGit(t, repo, "worktree", "add", "-q", "-b", branch, path)
	return path
}

func TestListWorktrees_AnnotatesSessionClaims(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if resp.RepoRoot == "" {
		t.Fatal("RepoRoot is empty for a git-backed project")
	}
	// The main checkout is listed for context and flagged as such.
	main := findWT(resp, p.Cwd)
	if main == nil {
		t.Fatalf("main checkout missing from inventory: %+v", resp.Worktrees)
	}
	if !main.IsMain {
		t.Errorf("main checkout IsMain = false")
	}

	got := findWT(resp, e.WorktreePath)
	if got == nil {
		t.Fatalf("session worktree %s missing: %+v", e.WorktreePath, resp.Worktrees)
	}
	if got.IsMain {
		t.Errorf("session worktree flagged IsMain")
	}
	if len(got.SessionIDs) != 1 || got.SessionIDs[0] != e.ID {
		t.Errorf("SessionIDs = %v, want [%s]", got.SessionIDs, e.ID)
	}
	if got.Branch != e.WorktreeBranch {
		t.Errorf("Branch = %q, want %q", got.Branch, e.WorktreeBranch)
	}
}

func TestListWorktrees_ReportsOrphanBranches(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	// A branch with a worktree must NOT appear as an orphan; one
	// without must.
	wt := newWorktree(t, p.Cwd, "has-tree")
	runGit(t, p.Cwd, "branch", "stranded")

	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if findOrphan(resp, "has-tree") != nil {
		t.Errorf("has-tree listed as orphaned though %s exists", wt)
	}
	if findOrphan(resp, "stranded") == nil {
		t.Errorf("stranded branch missing from orphans: %+v", resp.OrphanBranches)
	}
}

func TestListWorktrees_NonGitProjectIsEmptyNotAnError(t *testing.T) {
	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "plain", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees on a non-git project errored: %v", err)
	}
	if resp.RepoRoot != "" || len(resp.Worktrees) != 0 {
		t.Errorf("got %+v, want an empty inventory", resp)
	}
}

func TestListWorktrees_UnknownProject(t *testing.T) {
	r := freshRegistry(t)
	if _, err := r.ListWorktrees("nope"); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("got %v, want ErrProjectNotFound", err)
	}
}

// A live session inside the worktree blocks removal, and force does
// NOT override it — that refusal is absolute.
func TestRemoveWorktree_RefusesWhenLiveSessionInside(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	for _, force := range []bool{false, true} {
		err := r.RemoveWorktree(p.ID, e.WorktreePath, force, false)
		if !errors.Is(err, ErrWorktreeInUse) {
			t.Fatalf("force=%v: got %v, want ErrWorktreeInUse", force, err)
		}
		if _, serr := os.Stat(e.WorktreePath); serr != nil {
			t.Fatalf("force=%v: worktree removed despite refusal: %v", force, serr)
		}
	}
}

func TestRemoveWorktree_RefusesDirtyWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "dirty-tree")
	mustWriteFile(t, filepath.Join(wt, "scratch.txt"), "unsaved")

	if err := r.RemoveWorktree(p.ID, wt, false, false); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("got %v, want ErrWorktreeDirty", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree removed despite the refusal: %v", err)
	}
	if err := r.RemoveWorktree(p.ID, wt, true, false); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("worktree still present after forced remove")
	}
}

func TestRemoveWorktree_RefusesUnpushedWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "ahead-tree")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")

	// git status is clean here — only the unpushed check catches this.
	err := r.RemoveWorktree(p.ID, wt, false, false)
	if !errors.Is(err, ErrWorktreeUnpushed) {
		t.Fatalf("got %v, want ErrWorktreeUnpushed", err)
	}
	if _, serr := os.Stat(wt); serr != nil {
		t.Fatalf("worktree removed despite the refusal: %v", serr)
	}
}

func TestRemoveWorktree_KeepsBranchByDefault(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "keepme")

	if err := r.RemoveWorktree(p.ID, wt, false, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("worktree dir survived removal")
	}
	// The branch is the user's handle on the work — it stays unless
	// they ask for it to go, and it should now show up as an orphan.
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/keepme") == "" {
		t.Fatal("branch keepme was deleted without being asked for")
	}
	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if findOrphan(resp, "keepme") == nil {
		t.Errorf("keepme missing from orphan branches: %+v", resp.OrphanBranches)
	}
}

func TestRemoveWorktree_DeleteBranchOptionRemovesRef(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "goodbye")

	if err := r.RemoveWorktree(p.ID, wt, false, true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/goodbye") != "" {
		t.Errorf("branch goodbye still resolves after delete_branch")
	}
}

// The branch deleted is the worktree's own HEAD, never a caller-named
// one — otherwise a client could point at worktree A and delete
// branch B.
func TestRemoveWorktree_DeletesTheWorktreesOwnBranch(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "target")
	runGit(t, p.Cwd, "branch", "bystander")

	if err := r.RemoveWorktree(p.ID, wt, false, true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/bystander") == "" {
		t.Errorf("unrelated branch bystander was deleted")
	}
}

// Path containment: a client-supplied path outside <root>/.worktrees
// must never be touched, force or not.
func TestRemoveWorktree_RejectsPathOutsideWorktreesDir(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "precious.txt"), "do not delete")

	for _, path := range []string{
		outside,
		p.Cwd, // the main checkout itself
		filepath.Join(p.Cwd, ".worktrees"),
		filepath.Join(p.Cwd, ".worktrees", "..", "..", filepath.Base(outside)),
		filepath.Join(p.Cwd, ".worktrees-evil", "x"),
	} {
		if err := r.RemoveWorktree(p.ID, path, true, true); err == nil {
			t.Errorf("RemoveWorktree(%q) succeeded; want a containment refusal", path)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
		t.Fatalf("a file outside the worktrees dir was destroyed: %v", err)
	}
	if _, err := os.Stat(p.Cwd); err != nil {
		t.Fatalf("the main checkout was destroyed: %v", err)
	}
}

func TestCreateWorktreeForBranch_MaterializesOrphanedBranch(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	// An orphaned branch carrying a commit: exactly the "pick this
	// work back up" case.
	runGit(t, p.Cwd, "checkout", "-q", "-b", "stranded")
	mustWriteFile(t, filepath.Join(p.Cwd, "old-work.txt"), "important")
	runGit(t, p.Cwd, "add", "old-work.txt")
	runGit(t, p.Cwd, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "old work")
	runGit(t, p.Cwd, "checkout", "-q", "main")

	path, err := r.CreateWorktreeForBranch(context.Background(), p.ID, "stranded")
	if err != nil {
		t.Fatalf("CreateWorktreeForBranch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "old-work.txt")); err != nil {
		t.Errorf("the branch's content is not in the worktree: %v", err)
	}
	if got := gitOutput(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "stranded" {
		t.Errorf("worktree HEAD = %q, want stranded", got)
	}
	// It must no longer be an orphan.
	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if findOrphan(resp, "stranded") != nil {
		t.Errorf("stranded still listed as orphaned after materializing its worktree")
	}
	if findWT(resp, path) == nil {
		t.Errorf("new worktree %s missing from inventory", path)
	}
}

func TestCreateWorktreeForBranch_EmptyBranchRejected(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	if _, err := r.CreateWorktreeForBranch(context.Background(), p.ID, "  "); err == nil {
		t.Error("empty branch name accepted")
	}
}

func TestRenameWorktree_RenamesBranchAndDirectory(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "before")
	mustWriteFile(t, filepath.Join(wt, "keep.txt"), "content")
	runGit(t, wt, "add", "keep.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "keep")

	if err := r.RenameWorktree(p.ID, wt, "after"); err != nil {
		t.Fatalf("RenameWorktree: %v", err)
	}
	want := worktree.WorktreePath(p.Cwd, "after")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("renamed worktree %s missing: %v", want, err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("old worktree path %s still exists", wt)
	}
	if _, err := os.Stat(filepath.Join(want, "keep.txt")); err != nil {
		t.Errorf("content lost in the rename: %v", err)
	}
	if got := gitOutput(t, want, "rev-parse", "--abbrev-ref", "HEAD"); got != "after" {
		t.Errorf("branch = %q, want after", got)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/before") != "" {
		t.Errorf("old branch name still resolves")
	}
}

func TestRenameWorktree_RefusesWithLiveSession(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	if err := r.RenameWorktree(p.ID, e.WorktreePath, "renamed"); !errors.Is(err, ErrWorktreeInUse) {
		t.Fatalf("got %v, want ErrWorktreeInUse", err)
	}
	// The session's cwd must be untouched — that is the whole point.
	if _, err := os.Stat(e.WorktreePath); err != nil {
		t.Errorf("live session's worktree was moved: %v", err)
	}
}

// A rename onto a name that already exists must leave BOTH the branch
// and the directory exactly as they were — no half-applied state.
func TestRenameWorktree_RollsBackBranchWhenMoveFails(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "source")
	// Occupy the destination directory so `git worktree move` fails
	// after the branch rename has already happened.
	blocker := worktree.WorktreePath(p.Cwd, "dest")
	if err := os.MkdirAll(blocker, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := r.RenameWorktree(p.ID, wt, "dest"); err == nil {
		t.Fatal("rename onto an occupied path succeeded, want error")
	}
	if got := gitOutput(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "source" {
		t.Errorf("branch is %q after the failed rename, want source (rollback did not happen)", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("source worktree disturbed by the failed rename: %v", err)
	}
}

func TestRenameWorktree_RejectsPathOutsideWorktreesDir(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	if err := r.RenameWorktree(p.ID, t.TempDir(), "x"); err == nil {
		t.Error("rename of a path outside the worktrees dir succeeded")
	}
}

// Resuming work: a session created directly in an existing worktree
// must CLAIM it (Entry.WorktreePath set). An unclaimed worktree is
// what ReclaimOrphanWorktrees deletes at the next daemon start, so a
// miss here silently destroys the work the user just came back to.
func TestCreate_AdoptsExistingWorktreeWithoutSiblingSession(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "resumed")
	mustWriteFile(t, filepath.Join(wt, "wip.txt"), "half-done")

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", WorktreePath: wt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	if worktree.ResolvePath(e.WorktreePath) != worktree.ResolvePath(wt) {
		t.Fatalf("WorktreePath = %q, want %q — an unclaimed worktree gets reclaimed at next boot",
			e.WorktreePath, wt)
	}
	if e.WorktreeBranch != "resumed" {
		t.Errorf("WorktreeBranch = %q, want resumed", e.WorktreeBranch)
	}
	// No nested worktree stacked inside the one we adopted.
	if _, err := os.Stat(filepath.Join(wt, ".worktrees")); err == nil {
		t.Errorf("a nested .worktrees dir was created inside %s", wt)
	}
	// And the reclaim sweep must leave it alone now that it's claimed.
	r.ReclaimOrphanWorktrees()
	if _, err := os.Stat(filepath.Join(wt, "wip.txt")); err != nil {
		t.Fatalf("resumed worktree was reclaimed despite having a live session: %v", err)
	}
}

func TestReclaimOrphanWorktrees_KeepsNonPristine(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	dirty := newWorktree(t, p.Cwd, "detached-dirty")
	mustWriteFile(t, filepath.Join(dirty, "unsaved.txt"), "work")
	pristine := newWorktree(t, p.Cwd, "detached-clean")

	// Neither is claimed by a session — before this change both would
	// have been force-removed at startup.
	r.ReclaimOrphanWorktrees()

	if _, err := os.Stat(filepath.Join(dirty, "unsaved.txt")); err != nil {
		t.Errorf("worktree with uncommitted work was reclaimed: %v", err)
	}
	if _, err := os.Stat(pristine); err == nil {
		t.Errorf("pristine orphan %s survived the reclaim; the leak cleanup is broken", pristine)
	}
}

func TestReclaimOrphanWorktrees_KeepsUnpushedCommits(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "committed-work")
	mustWriteFile(t, filepath.Join(wt, "a.txt"), "work")
	runGit(t, wt, "add", "a.txt")
	runGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")

	r.ReclaimOrphanWorktrees()

	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree holding an unpushed commit was reclaimed: %v", err)
	}
}

// managedPath is the containment gate; exercise its edges directly.
func TestManagedPath(t *testing.T) {
	root := t.TempDir()
	ok := filepath.Join(root, ".worktrees", "feature")
	if err := os.MkdirAll(ok, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := managedPath(root, ok); err != nil {
		t.Errorf("managedPath rejected a legitimate worktree: %v", err)
	}
	// A symlink inside .worktrees/ pointing outside is the sharpest
	// version of the escape: the path looks managed until it is
	// resolved. managedPath resolves before comparing; pin that.
	escape := filepath.Join(root, ".worktrees", "escape")
	outside := t.TempDir()
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{
		"",
		root,
		filepath.Join(root, ".worktrees"),
		filepath.Join(root, ".worktrees-evil"),
		filepath.Join(root, "other"),
		filepath.Join(root, ".worktrees", "..", "..", "escape"),
		escape,
		filepath.Join(escape, "nested"),
		"/",
	} {
		if _, err := managedPath(root, bad); err == nil {
			t.Errorf("managedPath(%q) accepted; want refusal", bad)
		}
	}
}

// Refusal messages name the blocking sessions so the GUI can tell the
// user what to close.
func TestRemoveWorktree_InUseErrorNamesSessions(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	err = r.RemoveWorktree(p.ID, e.WorktreePath, false, false)
	if err == nil || !strings.Contains(err.Error(), e.ID) {
		t.Errorf("error %v does not name the blocking session %s", err, e.ID)
	}
}

// A worktree whose directory was deleted out-of-band (git still has an
// admin entry for it) must still be removable from the browser — it is
// exactly the leftover a user wants to sweep. This is where the path
// containment check used to refuse on macOS, because a missing path
// under /var and a repo root under /private/var don't compare equal.
func TestRemoveWorktree_CleansUpAStaleEntryWhoseDirIsGone(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "vanished")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	// Precondition: git still lists it.
	if !strings.Contains(gitOutput(t, p.Cwd, "worktree", "list"), "vanished") {
		t.Skip("git already pruned the entry; nothing stale to clean")
	}

	if err := r.RemoveWorktree(p.ID, wt, false, false); err != nil {
		t.Fatalf("RemoveWorktree on a stale entry: %v", err)
	}
	if strings.Contains(gitOutput(t, p.Cwd, "worktree", "list"), "vanished") {
		t.Errorf("stale worktree entry survived: %s", gitOutput(t, p.Cwd, "worktree", "list"))
	}
}

func TestDeleteBranch_RemovesAMergedOrphan(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	// Points at main, so it is merged by definition.
	runGit(t, p.Cwd, "branch", "tidy-me")

	if err := r.DeleteBranch(p.ID, "tidy-me", false); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/tidy-me") != "" {
		t.Errorf("branch still resolves after deletion")
	}
}

// The unmerged case is the one that loses work: git's own `branch -d`
// refuses it, and the client must get a specific code so it can ask
// before overriding.
func TestDeleteBranch_RefusesUnmergedWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	runGit(t, p.Cwd, "checkout", "-q", "-b", "has-work")
	mustWriteFile(t, filepath.Join(p.Cwd, "w.txt"), "work")
	runGit(t, p.Cwd, "add", "w.txt")
	runGit(t, p.Cwd, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "w")
	runGit(t, p.Cwd, "checkout", "-q", "main")

	err := r.DeleteBranch(p.ID, "has-work", false)
	if !errors.Is(err, ErrBranchUnmerged) {
		t.Fatalf("got %v, want ErrBranchUnmerged", err)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/has-work") == "" {
		t.Fatal("branch was deleted despite the refusal")
	}
	if err := r.DeleteBranch(p.ID, "has-work", true); err != nil {
		t.Fatalf("forced DeleteBranch: %v", err)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/has-work") != "" {
		t.Errorf("branch survived a forced delete")
	}
}

// A branch that still has a worktree goes through RemoveWorktree, so
// the ref and the directory can never disagree. force must not override.
func TestDeleteBranch_RefusesWhenTheBranchStillHasAWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	wt := newWorktree(t, p.Cwd, "occupied")

	for _, force := range []bool{false, true} {
		if err := r.DeleteBranch(p.ID, "occupied", force); !errors.Is(err, ErrBranchHasWorktree) {
			t.Fatalf("force=%v: got %v, want ErrBranchHasWorktree", force, err)
		}
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree disturbed by the refused branch delete: %v", err)
	}
	if gitOutput(t, p.Cwd, "rev-parse", "--verify", "--quiet", "refs/heads/occupied") == "" {
		t.Errorf("branch was deleted while its worktree still existed")
	}
}

func TestDeleteBranch_UnknownBranchAndEmptyName(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	if err := r.DeleteBranch(p.ID, "no-such-branch", true); err == nil {
		t.Error("deleting an unknown branch succeeded")
	}
	if err := r.DeleteBranch(p.ID, "   ", true); err == nil {
		t.Error("empty branch name accepted")
	}
}

// A project whose cwd IS a linked worktree — which is how hive gets run
// from inside its own .worktrees/. A plain session there must not claim
// that checkout as a hive-managed worktree, because Kill deletes what it
// claims: this reproduced as "close a session, lose your working
// directory and everything committed in it".
func TestCreate_DoesNotAdoptTheProjectsOwnCheckout(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	repo := initGitRepo(t)
	// Committed and merged, so the worktree is PRISTINE — precisely the
	// state in which Kill removes what it believes it owns.
	mustWriteFile(t, filepath.Join(repo, "precious.txt"), "the user's work")
	runGit(t, repo, "add", "precious.txt")
	runGit(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "work")
	linked := filepath.Join(repo, ".worktrees", "my-feature")
	runGit(t, repo, "worktree", "add", "-q", "-b", "my-feature", linked)

	p, err := r.CreateProject(wire.CreateProjectReq{Name: "inside-a-worktree", Cwd: linked})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	if e.WorktreePath != "" {
		t.Errorf("plain session claimed the project's own checkout: %q", e.WorktreePath)
	}
	if err := r.Kill(e.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(linked, "precious.txt")); err != nil {
		t.Fatalf("the user's checkout was deleted by closing a session: %v", err)
	}
}

// The same containment, one layer down: even if something upstream
// wrongly sets WorktreePath to a directory hive does not own, teardown
// must refuse to delete it. Kill is the last place a bug can still cost
// someone their working directory, so it does not trust the field.
func TestKill_RefusesToDeleteAnUnmanagedWorktreePath(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	// A worktree of the same repo, but outside .worktrees/ — the shape
	// of a checkout the user made themselves.
	outside := filepath.Join(t.TempDir(), "their-own-checkout")
	runGit(t, p.Cwd, "worktree", "add", "-q", "-b", "theirs", outside)
	mustWriteFile(t, filepath.Join(outside, "keep.txt"), "not hive's to delete")

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	// Force the bad state directly, standing in for any upstream bug.
	r.mu.Lock()
	r.entries[e.ID].WorktreePath = outside
	r.mu.Unlock()

	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("Kill deleted a worktree hive does not manage: %v", err)
	}
}

// projectRoot must resolve the MAIN checkout. When it resolved the
// linked worktree instead, .worktrees/ pointed at the wrong place and
// every legitimate path was refused — the browser was inert.
func TestListWorktrees_WorksWhenTheProjectCwdIsALinkedWorktree(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	repo := initGitRepo(t)
	linked := filepath.Join(repo, ".worktrees", "project-home")
	runGit(t, repo, "worktree", "add", "-q", "-b", "project-home", linked)
	sibling := filepath.Join(repo, ".worktrees", "sibling")
	runGit(t, repo, "worktree", "add", "-q", "-b", "sibling", sibling)

	p, err := r.CreateProject(wire.CreateProjectReq{Name: "linked", Cwd: linked})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	resp, err := r.ListWorktrees(p.ID)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if worktree.ResolvePath(resp.RepoRoot) != worktree.ResolvePath(repo) {
		t.Errorf("RepoRoot = %q, want the main checkout %q", resp.RepoRoot, repo)
	}
	// The real checkout is the main row, not the worktree the project
	// happens to sit in.
	main := findWT(resp, repo)
	if main == nil || !main.IsMain {
		t.Errorf("main checkout missing or unflagged: %+v", resp.Worktrees)
	}
	if row := findWT(resp, linked); row == nil || row.IsMain {
		t.Errorf("the project's own worktree should be an ordinary row: %+v", row)
	}
	// And removal of a sibling still works — managedPath's base was
	// wrong before, so this was refused outright.
	if err := r.RemoveWorktree(p.ID, sibling, false, false); err != nil {
		t.Fatalf("RemoveWorktree on a sibling worktree: %v", err)
	}
}
