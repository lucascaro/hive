//go:build darwin || windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinnedCheckout is a fakeGit answering as a checkout with one remote,
// origin, pointing at the pinned repository. What the checkout itself
// has checked out, and whether it is dirty, is deliberately absent:
// the latest channel no longer looks.
func pinnedCheckout() map[string]string {
	return map[string]string{
		"remote":                "origin",
		"remote get-url origin": "git@github.com:" + updateRepo + ".git",
	}
}

// ------------------------------ pinnedRemote ------------------------------

// The remote is found by URL, not by name and not through the current
// branch's upstream: a checkout on a feature branch with no upstream,
// or one where the pinned repository is called "upstream" and "origin"
// is a fork, still has exactly one remote worth building from.
func TestPinnedRemoteFindsTheRemoteByURL(t *testing.T) {
	g := &fakeGit{answers: map[string]string{
		"remote":                  "origin\nupstream",
		"remote get-url origin":   "https://github.com/someone-else/hive.git",
		"remote get-url upstream": "https://github.com/" + updateRepo,
	}}
	g.install(t)

	remote, err := pinnedRemote("/repo")
	if err != nil {
		t.Fatalf("pinnedRemote = %v, want the remote that points at %s", err, updateRepo)
	}
	if remote != "upstream" {
		t.Errorf("pinnedRemote = %q, want %q", remote, "upstream")
	}
	if g.ran("fetch") {
		t.Error("pinnedRemote fetched — it must only look")
	}
}

// A checkout with no remote at the pinned repository has nothing this
// button may pull and execute. That is a refusal in the user's terms,
// naming what to add, and nothing is fetched from the remotes it does
// have.
func TestPinnedRemoteRefusesWhenNoRemoteIsPinned(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]string
	}{
		{"no remotes", map[string]string{"remote": ""}},
		{"only a fork", map[string]string{
			"remote":                "origin",
			"remote get-url origin": "https://github.com/someone-else/hive.git",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGit{answers: tc.answers}
			g.install(t)

			_, err := pinnedRemote("/repo")
			if err == nil {
				t.Fatal("pinnedRemote = nil error with no pinned remote, want a refusal")
			}
			for _, want := range []string{"refusing to build", updateRepo} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if g.ran("fetch") {
				t.Error("pinnedRemote fetched from a remote nobody has vouched for")
			}
		})
	}
}

// ---------------------------- prepareBuildTree ----------------------------

// The first update has no build tree yet: it is added as a linked
// worktree of the checkout, detached at the pinned remote's main, with
// the checkout's own hooks disabled — `worktree add` runs post-checkout
// before build.sh gets a turn. The checkout itself is never pulled and
// never inspected: its branch, its upstream and its dirty files are not
// this code's business any more.
func TestPrepareBuildTreeAddsAWorktreeOnFirstUse(t *testing.T) {
	isolateStateDir(t)
	g := &fakeGit{answers: pinnedCheckout()}
	g.install(t)

	tree, err := prepareBuildTree("/repo", "origin")
	if err != nil {
		t.Fatalf("prepareBuildTree = %v, want nil", err)
	}
	if want := latestBuildTree(); tree != want {
		t.Errorf("prepareBuildTree = %q, want %q", tree, want)
	}
	add := firstCall(g, "worktree add")
	if add < 0 {
		t.Fatalf("prepareBuildTree never added a worktree; calls: %q", g.calls)
	}
	for _, want := range []string{"core.hooksPath=/dev/null", "--detach", tree, "origin/main"} {
		if !strings.Contains(g.calls[add], want) {
			t.Errorf("worktree add = %q, want it to carry %q", g.calls[add], want)
		}
	}
	if fetch := firstCall(g, "fetch"); fetch < 0 || add < fetch {
		t.Errorf("prepareBuildTree added the worktree (call %d) before fetching (call %d); calls: %q", add, fetch, g.calls)
	} else if !strings.Contains(g.calls[fetch], "origin") {
		t.Errorf("fetch = %q, want it to name the pinned remote", g.calls[fetch])
	}
	if prune := firstCall(g, "worktree prune"); prune < 0 || add < prune {
		t.Errorf("prepareBuildTree added the worktree (call %d) without pruning stale entries first (call %d); calls: %q", add, prune, g.calls)
	}
	for _, forbidden := range []string{"pull", "status", "symbolic-ref", "@{upstream}"} {
		if g.ran(forbidden) {
			t.Errorf("prepareBuildTree ran %q against the checkout — it must not touch or inspect it; calls: %q", forbidden, g.calls)
		}
	}
}

// Every later update reuses the tree: `npm ci` in a fresh worktree is
// minutes the user should pay once. Reuse is a checkout inside the
// tree, forced so anything a previous build left behind cannot block
// it, with hooks disabled the same as the add.
func TestPrepareBuildTreeReusesAnExistingWorktree(t *testing.T) {
	isolateStateDir(t)
	tree := latestBuildTree()
	writeFile(t, filepath.Join(tree, ".git"), "gitdir: /repo/.git/worktrees/latest-src\n")
	answers := pinnedCheckout()
	answers["worktree list --porcelain"] = "worktree /repo\nHEAD 0000000\nbranch refs/heads/main\n\nworktree " + tree + "\nHEAD 0000000\ndetached\n"
	g := &fakeGit{answers: answers}
	g.install(t)

	got, err := prepareBuildTree("/repo", "origin")
	if err != nil {
		t.Fatalf("prepareBuildTree = %v, want nil", err)
	}
	if got != tree {
		t.Errorf("prepareBuildTree = %q, want %q", got, tree)
	}
	if g.ran("worktree add") {
		t.Errorf("prepareBuildTree added a second worktree over an existing one; calls: %q", g.calls)
	}
	co := firstCall(g, "checkout")
	if co < 0 {
		t.Fatalf("prepareBuildTree never checked out the existing tree; calls: %q", g.calls)
	}
	for _, want := range []string{"core.hooksPath=/dev/null", "--detach", "--force", "origin/main"} {
		if !strings.Contains(g.calls[co], want) {
			t.Errorf("checkout = %q, want it to carry %q", g.calls[co], want)
		}
	}
	if !strings.HasPrefix(g.dirs[co], tree) {
		t.Errorf("checkout ran in %q, want the build tree %q", g.dirs[co], tree)
	}
}

// A tree that is a worktree of some *other* checkout — source_repo was
// pointed at a different clone since — shares nothing with this one:
// its <remote>/main is the old clone's. Checking it out would build
// the wrong repository, so it is replaced like any stray directory.
func TestPrepareBuildTreeReplacesATreeOfAnotherCheckout(t *testing.T) {
	isolateStateDir(t)
	tree := latestBuildTree()
	writeFile(t, filepath.Join(tree, ".git"), "gitdir: /elsewhere/.git/worktrees/latest-src\n")
	answers := pinnedCheckout()
	answers["worktree list --porcelain"] = "worktree /repo\nHEAD 0000000\nbranch refs/heads/main\n"
	g := &fakeGit{answers: answers}
	g.install(t)

	if _, err := prepareBuildTree("/repo", "origin"); err != nil {
		t.Fatalf("prepareBuildTree = %v, want nil", err)
	}
	if g.ran("checkout") {
		t.Errorf("prepareBuildTree checked out a tree belonging to another checkout; calls: %q", g.calls)
	}
	if !g.ran("worktree add") {
		t.Errorf("prepareBuildTree did not re-add the tree against this checkout; calls: %q", g.calls)
	}
}

// A directory at the tree's path that is not a worktree — a previous
// add that died half way, or a user who deleted .git by hand — cannot
// be checked out and blocks `worktree add`. It is the updater's own
// private directory, so it is replaced rather than refused.
func TestPrepareBuildTreeReplacesAStrayDirectory(t *testing.T) {
	isolateStateDir(t)
	tree := latestBuildTree()
	writeFile(t, filepath.Join(tree, "leftover.txt"), "x")
	g := &fakeGit{answers: pinnedCheckout()}
	g.install(t)

	if _, err := prepareBuildTree("/repo", "origin"); err != nil {
		t.Fatalf("prepareBuildTree = %v, want nil", err)
	}
	if !g.ran("worktree add") {
		t.Errorf("prepareBuildTree did not re-add the worktree over a stray directory; calls: %q", g.calls)
	}
	if _, err := os.Stat(filepath.Join(tree, "leftover.txt")); !os.IsNotExist(err) {
		t.Error("the stray directory's contents survived; worktree add would have refused it")
	}
}

// A fetch that fails (offline, auth, a remote that went away) leaves
// the build tree pointing at whatever was fetched last. Building that
// would report an "update" to something the user already has, so it is
// a refusal — in the user's terms, with git's words attached as the
// cause rather than pasted in front.
func TestPrepareBuildTreeRefusesWhenFetchFails(t *testing.T) {
	isolateStateDir(t)
	g := &fakeGit{
		answers: pinnedCheckout(),
		errs:    map[string]error{"fetch --quiet": fmt.Errorf("git fetch --quiet origin: fatal: unable to access 'https://github.com/'")},
	}
	g.install(t)

	_, err := prepareBuildTree("/repo", "origin")
	if err == nil {
		t.Fatal("prepareBuildTree = nil error when the fetch failed, want a refusal")
	}
	for _, want := range []string{"couldn't fetch origin", "check your network or credentials", "unable to access"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if strings.HasPrefix(err.Error(), "git ") {
		t.Errorf("error = %q, want a plain-language refusal, not raw git stderr", err)
	}
	if g.ran("worktree add") || g.ran("checkout") {
		t.Errorf("prepareBuildTree moved the build tree after a failed fetch; calls: %q", g.calls)
	}
}

// realPath resolves symlinks and cleans, so two spellings of one
// directory (macOS /private/var vs /var, Windows 8.3 names) compare
// equal.
func realPath(t *testing.T, p string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("resolve %s: %v", p, err)
	}
	return filepath.Clean(real)
}

// The real thing, end to end with git: a checkout parked on a dirty
// feature branch gets a build tree at the remote's main, is left
// exactly as it was, and a second run moves the tree to the new tip
// without adding a second worktree.
func TestPrepareBuildTreeWithRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	isolateStateDir(t)

	mustGit := func(dir string, args ...string) string {
		t.Helper()
		out, err := runGit(dir, args...)
		if err != nil {
			t.Fatalf("%v", err)
		}
		return out
	}
	commit := func(dir, msg string) {
		t.Helper()
		mustGit(dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", msg)
	}

	// A bare remote, seeded with one commit on main.
	remote := t.TempDir()
	mustGit(remote, "init", "-q", "--bare", "-b", "main")
	seed := t.TempDir()
	mustGit(seed, "init", "-q", "-b", "main")
	commit(seed, "seed")
	mustGit(seed, "remote", "add", "origin", remote)
	mustGit(seed, "push", "-q", "origin", "main")

	// The user's checkout: cloned, then parked on a feature branch with
	// a local commit and an uncommitted file — every state the old
	// updater refused.
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	mustGit(parent, "clone", "-q", remote, repo)
	mustGit(repo, "checkout", "-q", "-b", "feature")
	commit(repo, "local work")
	writeFile(t, filepath.Join(repo, "scratch.txt"), "uncommitted")

	tree, err := prepareBuildTree(repo, "origin")
	if err != nil {
		t.Fatalf("prepareBuildTree = %v, want nil", err)
	}
	if got, want := mustGit(tree, "rev-parse", "HEAD"), mustGit(repo, "rev-parse", "origin/main"); got != want {
		t.Errorf("build tree HEAD = %s, want origin/main = %s", got, want)
	}
	if st, err := os.Stat(filepath.Join(tree, ".git")); err != nil || st.IsDir() {
		t.Errorf("build tree is not a linked worktree of the checkout (.git: %v)", err)
	}
	if got := mustGit(repo, "symbolic-ref", "--short", "HEAD"); got != "feature" {
		t.Errorf("checkout moved to %q, want it left on feature", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "scratch.txt")); err != nil {
		t.Errorf("checkout's uncommitted file is gone: %v", err)
	}

	// The remote moves on; the second run follows it in the same tree.
	other := filepath.Join(parent, "other")
	mustGit(parent, "clone", "-q", remote, other)
	commit(other, "upstream moved")
	mustGit(other, "push", "-q", "origin", "main")
	fresh := mustGit(other, "rev-parse", "HEAD")

	again, err := prepareBuildTree(repo, "origin")
	if err != nil {
		t.Fatalf("second prepareBuildTree = %v, want nil", err)
	}
	if again != tree {
		t.Errorf("second run used %q, want the same tree %q", again, tree)
	}
	if got := mustGit(tree, "rev-parse", "HEAD"); got != fresh {
		t.Errorf("build tree HEAD = %s after the remote moved, want %s", got, fresh)
	}
	if n := strings.Count(mustGit(repo, "worktree", "list", "--porcelain"), "worktree "); n != 2 {
		t.Errorf("checkout has %d worktrees, want 2 (itself and the build tree)", n)
	}

	// Pointing source_repo at a different clone must not keep building
	// the old one: the tree is re-added against the new checkout.
	second := filepath.Join(parent, "second")
	mustGit(parent, "clone", "-q", remote, second)
	moved, err := prepareBuildTree(second, "origin")
	if err != nil {
		t.Fatalf("prepareBuildTree(second clone) = %v, want nil", err)
	}
	if moved != tree {
		t.Errorf("second clone used %q, want the same tree path %q", moved, tree)
	}
	// `worktree list` from inside a linked tree names its main worktree
	// first; that is the checkout the tree belongs to.
	first, _, _ := strings.Cut(mustGit(tree, "worktree", "list", "--porcelain"), "\n")
	owner := strings.TrimPrefix(first, "worktree ")
	if got, want := realPath(t, owner), realPath(t, second); !strings.EqualFold(got, want) {
		t.Errorf("build tree belongs to %s, want the second clone's %s", got, want)
	}
	// The first clone keeps a stale registration for the path; that is
	// its own bookkeeping, which its next `git worktree prune` (or gc)
	// clears, and nothing the updater can reach from the second clone.
}
