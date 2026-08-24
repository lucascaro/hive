package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// findInfo returns the Info whose path matches want (symlink-resolved),
// or nil. macOS reports /private/var where t.TempDir hands back /var.
func findInfo(list []Info, want string) *Info {
	want = ResolvePath(want)
	for i := range list {
		if list[i].Path == want {
			return &list[i]
		}
	}
	return nil
}

func findBranch(list []BranchInfo, name string) *BranchInfo {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func TestList_ReportsWorktreesWithBranches(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "feature-a")
	if err := CreateWorktree(context.Background(), repo, "feature-a", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	list, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List returned %d entries, want 2 (main + worktree): %+v", len(list), list)
	}
	if main := findInfo(list, repo); main == nil {
		t.Errorf("main checkout %q missing from List: %+v", repo, list)
	}
	got := findInfo(list, wt)
	if got == nil {
		t.Fatalf("worktree %q missing from List: %+v", wt, list)
	}
	if got.Branch != "feature-a" {
		t.Errorf("Branch = %q, want feature-a", got.Branch)
	}
	if got.Detached {
		t.Errorf("Detached = true, want false")
	}
	if got.Head == "" {
		t.Errorf("Head is empty")
	}
}

func TestList_DetachedHeadHasNoBranch(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(repo, ".worktrees", "detached")
	head := revParse(t, repo, "HEAD")
	mustGit(t, repo, "worktree", "add", "--detach", wt, head)

	list, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := findInfo(list, wt)
	if got == nil {
		t.Fatalf("detached worktree missing from List: %+v", list)
	}
	if !got.Detached {
		t.Errorf("Detached = false, want true")
	}
	if got.Branch != "" {
		t.Errorf("Branch = %q, want empty for a detached worktree", got.Branch)
	}
}

// parseWorktreeList is exercised directly for the shapes a real repo
// makes awkward to produce (locked, prunable).
func TestParseWorktreeList_FlagsLockedAndPrunable(t *testing.T) {
	out := strings.Join([]string{
		"worktree /repo",
		"HEAD abc123",
		"branch refs/heads/main",
		"",
		"worktree /repo/.worktrees/locked",
		"HEAD def456",
		"branch refs/heads/locked-one",
		"locked",
		"",
		"worktree /repo/.worktrees/gone",
		"HEAD 000000",
		"detached",
		"prunable gitdir file points to non-existent location",
		"",
	}, "\n")
	list := parseWorktreeList(out)
	if len(list) != 3 {
		t.Fatalf("parsed %d records, want 3: %+v", len(list), list)
	}
	if list[0].Branch != "main" {
		t.Errorf("record 0 Branch = %q, want main", list[0].Branch)
	}
	if !list[1].Locked {
		t.Errorf("record 1 Locked = false, want true")
	}
	if !list[2].Prunable || !list[2].Detached {
		t.Errorf("record 2 = %+v, want prunable+detached", list[2])
	}
}

func TestListBranches_FlagsBranchesWithoutWorktree(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "has-tree")
	if err := CreateWorktree(context.Background(), repo, "has-tree", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	// An orphan: a branch with no worktree anywhere.
	mustGit(t, repo, "branch", "orphan")

	branches, err := ListBranches(repo)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	withTree := findBranch(branches, "has-tree")
	if withTree == nil {
		t.Fatalf("has-tree missing: %+v", branches)
	}
	if !withTree.HasWorktree {
		t.Errorf("has-tree HasWorktree = false, want true")
	}
	orphan := findBranch(branches, "orphan")
	if orphan == nil {
		t.Fatalf("orphan missing: %+v", branches)
	}
	if orphan.HasWorktree {
		t.Errorf("orphan HasWorktree = true, want false")
	}
	if orphan.Upstream != "" {
		t.Errorf("orphan Upstream = %q, want empty", orphan.Upstream)
	}
}

func TestListBranches_MarksMergedIntoDefault(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	// merged-branch points at the same commit as main, so it is
	// reachable from origin/main by definition.
	mustGit(t, repo, "branch", "merged-branch")
	// diverged carries a commit origin/main does not have.
	mustGit(t, repo, "checkout", "-q", "-b", "diverged")
	mustWrite(t, filepath.Join(repo, "new.txt"), "work")
	mustGit(t, repo, "add", "new.txt")
	mustGit(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "work")
	mustGit(t, repo, "checkout", "-q", "main")

	branches, err := ListBranches(repo)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	merged := findBranch(branches, "merged-branch")
	if merged == nil || !merged.Merged {
		t.Errorf("merged-branch Merged = %v, want true", merged)
	}
	div := findBranch(branches, "diverged")
	if div == nil {
		t.Fatalf("diverged missing: %+v", branches)
	}
	if div.Merged {
		t.Errorf("diverged Merged = true, want false")
	}
	if div.Ahead != 1 {
		t.Errorf("diverged Ahead = %d, want 1", div.Ahead)
	}
}

func TestListBranches_DetectsSquashMerge(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	// Catch local main up with the upstream tip so the squash commit
	// can be pushed later.
	mustGit(t, repo, "fetch", "-q", "origin")
	mustGit(t, repo, "merge", "-q", "--ff-only", "origin/main")

	// A branch with two commits, the shape a squash merge collapses.
	mustGit(t, repo, "checkout", "-q", "-b", "squashed")
	mustWrite(t, filepath.Join(repo, "a.txt"), "one")
	mustGit(t, repo, "add", "a.txt")
	mustGit(t, repo, "commit", "-q", "-m", "one")
	mustWrite(t, filepath.Join(repo, "b.txt"), "two")
	mustGit(t, repo, "add", "b.txt")
	mustGit(t, repo, "commit", "-q", "-m", "two")

	// Squash it onto main and publish — exactly what a GitHub squash
	// merge leaves behind: same tree, unrelated commit, branch not an
	// ancestor of origin/main.
	mustGit(t, repo, "checkout", "-q", "main")
	mustGit(t, repo, "merge", "-q", "--squash", "squashed")
	mustGit(t, repo, "commit", "-q", "-m", "squashed (#1)")
	mustGit(t, repo, "push", "-q", "origin", "main")

	// A branch whose work is genuinely not upstream.
	mustGit(t, repo, "checkout", "-q", "-b", "diverged")
	mustWrite(t, filepath.Join(repo, "c.txt"), "three")
	mustGit(t, repo, "add", "c.txt")
	mustGit(t, repo, "commit", "-q", "-m", "three")
	mustGit(t, repo, "checkout", "-q", "main")

	branches, err := ListBranches(repo)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	sq := findBranch(branches, "squashed")
	if sq == nil {
		t.Fatalf("squashed missing: %+v", branches)
	}
	if !sq.Merged {
		t.Errorf("squashed Merged = false, want true (Ahead=%d)", sq.Ahead)
	}
	div := findBranch(branches, "diverged")
	if div == nil {
		t.Fatalf("diverged missing: %+v", branches)
	}
	if div.Merged {
		t.Errorf("diverged Merged = true, want false")
	}
}

func TestInspect_DetectsSquashMergedWorktree(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	mustGit(t, repo, "fetch", "-q", "origin")
	mustGit(t, repo, "merge", "-q", "--ff-only", "origin/main")

	wt := WorktreePath(repo, "done")
	mustGit(t, repo, "worktree", "add", "-q", "-b", "done", wt, "main")
	mustWrite(t, filepath.Join(wt, "a.txt"), "one")
	mustGit(t, wt, "add", "a.txt")
	mustGit(t, wt, "commit", "-q", "-m", "one")

	mustGit(t, repo, "merge", "-q", "--squash", "done")
	mustGit(t, repo, "commit", "-q", "-m", "done (#1)")
	mustGit(t, repo, "push", "-q", "origin", "main")

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Merged {
		t.Errorf("Merged = false, want true (%+v)", s)
	}
	// Pristine deliberately ignores Merged: the unconfirmed auto-delete
	// paths (session Kill, boot reclaim) gate on it, and a heuristic
	// must not widen those. The browser's own delete consults Merged
	// separately.
	if s.Pristine() {
		t.Errorf("Pristine() = true, want false — Merged must not widen it (%+v)", s)
	}
}

func TestInspect_CleanWorktreeIsPristine(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	wt := WorktreePath(repo, "clean")
	// Branch straight off origin/main so there is nothing ahead.
	mustGit(t, repo, "worktree", "add", "-q", "-b", "clean", wt, "origin/main")

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Pristine() {
		t.Errorf("Pristine() = false, want true (%+v)", s)
	}
	if s.Branch != "clean" {
		t.Errorf("Branch = %q, want clean", s.Branch)
	}
}

func TestInspect_UncommittedChangesNotPristine(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	wt := WorktreePath(repo, "dirty")
	mustGit(t, repo, "worktree", "add", "-q", "-b", "dirty", wt, "origin/main")
	mustWrite(t, filepath.Join(wt, "scratch.txt"), "unsaved")

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Uncommitted {
		t.Errorf("Uncommitted = false, want true")
	}
	if s.Pristine() {
		t.Errorf("Pristine() = true, want false (%+v)", s)
	}
}

func TestInspect_UnpushedCommitsCounted(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	wt := WorktreePath(repo, "ahead")
	mustGit(t, repo, "worktree", "add", "-q", "-b", "ahead", wt, "origin/main")
	mustWrite(t, filepath.Join(wt, "a.txt"), "committed work")
	mustGit(t, wt, "add", "a.txt")
	mustGit(t, wt, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a")

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if s.Uncommitted {
		t.Errorf("Uncommitted = true, want false (the work is committed)")
	}
	if s.Unpushed != 1 {
		t.Errorf("Unpushed = %d, want 1", s.Unpushed)
	}
	if s.Pristine() {
		t.Errorf("Pristine() = true, want false — committed-but-unpushed work would be lost")
	}
}

// The conservative direction: with no upstream and no resolvable
// default ref, Inspect must refuse to claim the worktree is disposable.
func TestInspect_NoUpstreamNoBaseIsUnknown(t *testing.T) {
	repo := initRepo(t)
	// Rename the only branch to something none of defaultRef's
	// candidates match, so no base resolves at all.
	mustGit(t, repo, "branch", "-m", "main", "trunk")
	wt := WorktreePath(repo, "nobase")
	if err := CreateWorktree(context.Background(), repo, "nobase", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Unknown {
		t.Errorf("Unknown = false, want true (%+v)", s)
	}
	if s.Pristine() {
		t.Errorf("Pristine() = true, want false — an unanswerable base must not read as safe")
	}
}

func TestInspect_MissingWorktreeIsPristine(t *testing.T) {
	repo := initRepo(t)
	s, err := Inspect(repo, filepath.Join(repo, ".worktrees", "never-existed"))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Pristine() {
		t.Errorf("Pristine() = false, want true for a missing worktree (%+v)", s)
	}
}

func TestInspect_DetachedWorktreeIsUnknown(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	wt := filepath.Join(repo, ".worktrees", "detached")
	mustGit(t, repo, "worktree", "add", "-q", "--detach", wt, "origin/main")

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if s.Branch != "" {
		t.Errorf("Branch = %q, want empty", s.Branch)
	}
	if !s.Unknown || s.Pristine() {
		t.Errorf("detached worktree = %+v, want Unknown and not Pristine", s)
	}
}

func TestMoveWorktree_RelocatesDirectory(t *testing.T) {
	repo := initRepo(t)
	from := WorktreePath(repo, "before")
	if err := CreateWorktree(context.Background(), repo, "before", from); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	to := WorktreePath(repo, "after")

	if err := MoveWorktree(repo, from, to); err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Errorf("old path %s still exists", from)
	}
	if _, err := os.Stat(to); err != nil {
		t.Errorf("new path %s missing: %v", to, err)
	}
	list, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if findInfo(list, to) == nil {
		t.Errorf("git admin state not updated; %s absent from List: %+v", to, list)
	}
}

func TestMoveWorktree_RefusesExistingDestination(t *testing.T) {
	repo := initRepo(t)
	from := WorktreePath(repo, "src")
	if err := CreateWorktree(context.Background(), repo, "src", from); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	to := WorktreePath(repo, "taken")
	mustMkdir(t, to)

	if err := MoveWorktree(repo, from, to); err == nil {
		t.Fatal("MoveWorktree onto an existing path succeeded, want error")
	}
	if _, err := os.Stat(from); err != nil {
		t.Errorf("source was disturbed by the refused move: %v", err)
	}
}

func TestRenameBranch_MovesRef(t *testing.T) {
	repo := initRepo(t)
	mustGit(t, repo, "branch", "old-name")
	before := revParse(t, repo, "old-name")

	if err := RenameBranch(repo, "old-name", "new-name"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if got := revParse(t, repo, "new-name"); got != before {
		t.Errorf("new-name = %s, want %s", got, before)
	}
	if err := refResolves(repo, "old-name"); err == nil {
		t.Errorf("old-name still resolves after rename")
	}
}

// refResolves reports whether a ref resolves, without failing the test.
func refResolves(repo, ref string) error {
	_, err := git(context.Background(), readTimeout, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+ref)
	return err
}

func TestRenameBranch_RefusesExistingName(t *testing.T) {
	repo := initRepo(t)
	mustGit(t, repo, "branch", "one")
	mustGit(t, repo, "branch", "two")
	if err := RenameBranch(repo, "one", "two"); err == nil {
		t.Fatal("RenameBranch onto an existing branch succeeded, want error")
	}
	if err := refResolves(repo, "one"); err != nil {
		t.Errorf("source branch lost after refused rename: %v", err)
	}
}

func TestDeleteBranch_RefusesUnmergedWithoutForce(t *testing.T) {
	repo := initRepo(t)
	mustGit(t, repo, "checkout", "-q", "-b", "unmerged")
	mustWrite(t, filepath.Join(repo, "x.txt"), "work")
	mustGit(t, repo, "add", "x.txt")
	mustGit(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "x")
	mustGit(t, repo, "checkout", "-q", "main")

	if err := DeleteBranch(repo, "unmerged", false); err == nil {
		t.Fatal("DeleteBranch(force=false) on unmerged branch succeeded, want refusal")
	}
	if err := refResolves(repo, "unmerged"); err != nil {
		t.Fatalf("branch was deleted despite the refusal: %v", err)
	}
	if err := DeleteBranch(repo, "unmerged", true); err != nil {
		t.Fatalf("DeleteBranch(force=true): %v", err)
	}
	if err := refResolves(repo, "unmerged"); err == nil {
		t.Errorf("branch still resolves after forced delete")
	}
}

// Root reports a linked worktree's own top level; MainRoot must see
// past that to the real checkout. Callers that confuse the two end up
// treating a worktree as if it were the repo.
func TestMainRoot_ResolvesFromInsideALinkedWorktree(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "linked")
	if err := CreateWorktree(context.Background(), repo, "linked", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if got, err := Root(wt); err != nil || ResolvePath(got) != ResolvePath(wt) {
		t.Fatalf("precondition: Root(worktree) = %q (%v), want the worktree itself", got, err)
	}
	got, err := MainRoot(wt)
	if err != nil {
		t.Fatalf("MainRoot: %v", err)
	}
	if got != ResolvePath(repo) {
		t.Errorf("MainRoot(%q) = %q, want %q", wt, got, ResolvePath(repo))
	}
	// And from the main checkout it is a no-op.
	got, err = MainRoot(repo)
	if err != nil {
		t.Fatalf("MainRoot(main): %v", err)
	}
	if got != ResolvePath(repo) {
		t.Errorf("MainRoot(main) = %q, want %q", got, ResolvePath(repo))
	}
}

// A worktree directory that is already gone must still resolve
// consistently with its repo root, or the registry's containment check
// refuses to clean up the stale entry. On macOS the repo root resolves
// to /private/var while a missing path under it does not, which is how
// this diverges.
func TestResolvePath_MissingLeafStillResolvesUnderItsRoot(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "vanished")
	if err := CreateWorktree(context.Background(), repo, "vanished", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	resolvedWhilePresent := ResolvePath(wt)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	got := ResolvePath(wt)
	if got != resolvedWhilePresent {
		t.Errorf("ResolvePath after deletion = %q, want %q (same as before)",
			got, resolvedWhilePresent)
	}
	root := ResolvePath(repo)
	rel, err := filepath.Rel(root, got)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("resolved missing path %q is not under repo root %q (rel=%q, err=%v)",
			got, root, rel, err)
	}
}

func TestResolvePath_EmptyAndRoot(t *testing.T) {
	if ResolvePath("") != "" {
		t.Error(`ResolvePath("") should stay empty`)
	}
	// The property under test is termination: ResolvePath recurses on
	// its parent directory, so a filesystem root must be a fixed point
	// rather than recursing forever. The exact spelling is
	// platform-dependent (Windows normalizes "/" to `\`), so assert
	// non-empty and stable rather than an exact separator.
	root := filepath.Clean(string(filepath.Separator))
	got := ResolvePath(root)
	if got == "" {
		t.Fatalf("ResolvePath(%q) returned empty", root)
	}
	if again := ResolvePath(got); again != got {
		t.Errorf("ResolvePath is not stable at the root: %q -> %q", got, again)
	}
}

// Guard paths: these are the branches a caller hits by passing
// something wrong, and they must fail cleanly rather than shelling out
// to git with an empty argument.
func TestInventoryGuards(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)

	t.Run("MoveWorktree rejects empty paths", func(t *testing.T) {
		if err := MoveWorktree(repo, "", "/x"); err == nil {
			t.Error("empty source accepted")
		}
		if err := MoveWorktree(repo, "/x", ""); err == nil {
			t.Error("empty destination accepted")
		}
	})

	t.Run("RenameBranch rejects empty names", func(t *testing.T) {
		if err := RenameBranch(repo, "", "x"); err == nil {
			t.Error("empty source accepted")
		}
		if err := RenameBranch(repo, "x", ""); err == nil {
			t.Error("empty destination accepted")
		}
	})

	// Renaming to the current name is a no-op, not an error: the UI
	// commits on Enter even when the text is unchanged.
	t.Run("RenameBranch to the same name is a no-op", func(t *testing.T) {
		mustGit(t, repo, "branch", "same")
		before := revParse(t, repo, "same")
		if err := RenameBranch(repo, "same", "same"); err != nil {
			t.Fatalf("same-name rename: %v", err)
		}
		if got := revParse(t, repo, "same"); got != before {
			t.Errorf("branch moved: %s -> %s", before, got)
		}
	})

	t.Run("DeleteBranch rejects an empty name", func(t *testing.T) {
		if err := DeleteBranch(repo, "", true); err == nil {
			t.Error("empty branch name accepted")
		}
	})

	// Every read path must report an error rather than panic or return
	// a half-built list when pointed at something that is not a repo.
	t.Run("read paths fail cleanly outside a repo", func(t *testing.T) {
		notRepo := t.TempDir()
		if _, err := List(notRepo); err == nil {
			t.Error("List succeeded outside a repo")
		}
		if _, err := ListBranches(notRepo); err == nil {
			t.Error("ListBranches succeeded outside a repo")
		}
	})
}

// aheadCount's unresolvable-base path: an empty base means "no
// comparison possible", which must read as 0 rather than guessing.
// An unanswerable count must report ok=false, not 0. A caller that
// cannot tell those apart deletes commits it thinks are not there.
func TestAheadCount_UnresolvableBase(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	if got, ok := aheadCount(repo, "main", ""); got != 0 || ok {
		t.Errorf("aheadCount with no base = (%d, %v), want (0, false)", got, ok)
	}
	if got, ok := aheadCount(repo, "main", "no-such-ref"); got != 0 || ok {
		t.Errorf("aheadCount against a missing ref = (%d, %v), want (0, false)", got, ok)
	}
	// The answerable case still answers.
	if got, ok := aheadCount(repo, "main", "main"); got != 0 || !ok {
		t.Errorf("aheadCount(main..main) = (%d, %v), want (0, true)", got, ok)
	}
}

// parseWorktreeList must not fall over on the shapes git can emit.
func TestParseWorktreeList_EdgeShapes(t *testing.T) {
	if got := parseWorktreeList(""); len(got) != 0 {
		t.Errorf("empty input produced %d records", len(got))
	}
	// No trailing blank line — the final record still has to flush.
	got := parseWorktreeList("worktree /a\nHEAD abc\nbranch refs/heads/b")
	if len(got) != 1 || got[0].Branch != "b" {
		t.Errorf("unterminated record parsed as %+v", got)
	}
	// CRLF, as git emits under some Windows configurations.
	got = parseWorktreeList("worktree /a\r\nHEAD abc\r\nbranch refs/heads/b\r\n")
	if len(got) != 1 || got[0].Branch != "b" {
		t.Errorf("CRLF record parsed as %+v", got)
	}
}

// A stat failure is not the same as "there is nothing there". Only a
// genuinely missing directory is disposable; anything we could not look
// at must read as holding work, or the caller deletes it.
func TestInspect_UnreadableWorktreeIsNotPristine(t *testing.T) {
	skipNoGit(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny access")
	}
	repo := initRepo(t)
	wt := WorktreePath(repo, "unreadable")
	if err := CreateWorktree(context.Background(), repo, "unreadable", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	// Deny traversal of the parent so stat on the worktree fails with
	// EACCES rather than ENOENT.
	parent := filepath.Dir(wt)
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	s, err := Inspect(repo, wt)
	if err == nil {
		t.Error("Inspect on an unreadable worktree returned no error")
	}
	if !s.Unknown {
		t.Errorf("Unknown = false for an unreadable worktree (%+v)", s)
	}
	if s.Pristine() {
		t.Error("an unreadable worktree read as pristine — it would be deleted")
	}
}

// IsManaged is the single definition of "a worktree hive owns". Every
// destructive path asks it, so its edges are the containment contract.
func TestIsManaged(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, ".worktrees", "feature")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsManaged(root, managed) {
		t.Errorf("IsManaged rejected a legitimate worktree %q", managed)
	}
	// A nested path under a managed worktree is still inside the
	// managed dir, which is what the removal guard cares about.
	if !IsManaged(root, filepath.Join(managed, "sub")) {
		t.Errorf("IsManaged rejected a path nested under a managed worktree")
	}
	for _, bad := range []string{
		"",
		root,
		filepath.Join(root, ".worktrees"),
		// A prefix test would wrongly accept this one.
		filepath.Join(root, ".worktrees-evil"),
		filepath.Join(root, "other"),
		filepath.Join(root, ".worktrees", "..", "..", "escape"),
		t.TempDir(),
	} {
		if IsManaged(root, bad) {
			t.Errorf("IsManaged accepted %q", bad)
		}
	}
	if IsManaged("", managed) {
		t.Error("IsManaged accepted an empty root")
	}
}

// A symlink inside .worktrees/ pointing outside must not read as
// managed — otherwise it is a way to aim the remover at any directory.
func TestIsManaged_RejectsASymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".worktrees", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if IsManaged(root, link) {
		t.Errorf("IsManaged accepted a symlink escaping to %q", outside)
	}
}

// Locked and prunable are states a real repo produces and a fabricated
// fixture does not. Both are reported by `git worktree list`, and both
// change what the browser should let the user do.
func TestList_ReportsLockedWorktrees(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "locked-one")
	if err := CreateWorktree(context.Background(), repo, "locked-one", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	mustGit(t, repo, "worktree", "lock", wt)

	list, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := findInfo(list, wt)
	if got == nil {
		t.Fatalf("locked worktree missing from List: %+v", list)
	}
	if !got.Locked {
		t.Errorf("Locked = false for a locked worktree: %+v", got)
	}
	// Unlocking clears it again.
	mustGit(t, repo, "worktree", "unlock", wt)
	list, _ = List(repo)
	if got = findInfo(list, wt); got == nil || got.Locked {
		t.Errorf("Locked still set after unlock: %+v", got)
	}
}

// A worktree whose directory was deleted out-of-band: git keeps the
// admin entry and marks it prunable. This is the leftover a user most
// wants to sweep from the browser.
func TestList_ReportsPrunableWorktrees(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "vanished")
	if err := CreateWorktree(context.Background(), repo, "vanished", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	list, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := findInfo(list, wt)
	if got == nil {
		t.Fatalf("prunable worktree missing from List: %+v", list)
	}
	if !got.Prunable {
		t.Errorf("Prunable = false for a worktree whose dir is gone: %+v", got)
	}
	// And it inspects as pristine — nothing on disk left to lose — so
	// the browser can offer to clean it up without a warning.
	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Pristine() {
		t.Errorf("a vanished worktree should be disposable: %+v", s)
	}
}

// A locked worktree still reports its real status: locking is git's
// "do not prune this", not a claim about the contents.
func TestInspect_LockedWorktreeStillReportsItsWork(t *testing.T) {
	repo := initRepo(t)
	wt := WorktreePath(repo, "locked-dirty")
	if err := CreateWorktree(context.Background(), repo, "locked-dirty", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	mustWrite(t, filepath.Join(wt, "wip.txt"), "unsaved")
	mustGit(t, repo, "worktree", "lock", wt)
	t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "worktree", "unlock", wt).Run() })

	s, err := Inspect(repo, wt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !s.Uncommitted || s.Pristine() {
		t.Errorf("locked worktree with uncommitted work read as %+v", s)
	}
}

func TestDeleteRemoteBranch_RemovesTheRefFromTheRemote(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	mustGit(t, repo, "fetch", "-q", "origin")
	mustGit(t, repo, "checkout", "-q", "-b", "publish-me")
	mustWrite(t, filepath.Join(repo, "a.txt"), "work")
	mustGit(t, repo, "add", "a.txt")
	mustGit(t, repo, "commit", "-q", "-m", "work")
	mustGit(t, repo, "push", "-q", "-u", "origin", "publish-me")

	remote, remoteBranch := UpstreamOf(repo, "publish-me")
	if remote != "origin" || remoteBranch != "publish-me" {
		t.Fatalf("UpstreamOf = %q/%q, want origin/publish-me", remote, remoteBranch)
	}
	if err := DeleteRemoteBranch(repo, remote, remoteBranch); err != nil {
		t.Fatalf("DeleteRemoteBranch: %v", err)
	}
	out, err := exec.Command("git", "-C", repo, "ls-remote", "--heads", "origin", "publish-me").Output()
	if err != nil {
		t.Fatalf("ls-remote: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("remote branch still present: %s", out)
	}

	// Deleting it again is not an error: someone else getting there
	// first (GitHub's delete-on-merge) leaves the asked-for end state.
	if err := DeleteRemoteBranch(repo, remote, remoteBranch); err != nil {
		t.Errorf("second delete: %v, want nil", err)
	}
}

func TestUpstreamOf_EmptyWithoutTracking(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	mustGit(t, repo, "branch", "untracked")
	if remote, branch := UpstreamOf(repo, "untracked"); remote != "" || branch != "" {
		t.Errorf("UpstreamOf = %q/%q, want empty", remote, branch)
	}
}

func TestScrubURLCredentials(t *testing.T) {
	in := "git push: remote: fatal: https://user:ghp_secrettoken@github.com/o/r.git rejected"
	got := scrubURLCredentials(in)
	if strings.Contains(got, "ghp_secrettoken") {
		t.Errorf("token survived scrubbing: %s", got)
	}
	if !strings.Contains(got, "//***@github.com") {
		t.Errorf("unexpected scrub result: %s", got)
	}
}
