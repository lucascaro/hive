package worktree

import (
	"context"
	"os"
	"path/filepath"
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
	// Must terminate rather than recurse forever on a filesystem root.
	if got := ResolvePath("/"); got != "/" {
		t.Errorf(`ResolvePath("/") = %q, want /`, got)
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
func TestAheadCount_UnresolvableBase(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	if got := aheadCount(repo, "main", ""); got != 0 {
		t.Errorf("aheadCount with no base = %d, want 0", got)
	}
	if got := aheadCount(repo, "main", "no-such-ref"); got != 0 {
		t.Errorf("aheadCount against a missing ref = %d, want 0", got)
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
