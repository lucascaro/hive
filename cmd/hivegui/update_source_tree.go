//go:build darwin || windows

package main

// The latest channel's source tree. Tagged like stageLatest, its only
// caller: on Linux the channel has a check (update_latest.go, which
// uses pinnedRemote from update_remote.go) but no in-place apply, so
// nothing here would run.
//
// The channel used to pull and build the user's own checkout, which
// made the user's branch the updater's business: a feature branch with
// no upstream, a dirty tree, a detached HEAD or a branch that had
// diverged from main each refused the update, and a Hive running out of
// the checkout's own build directory collided with `wails build -clean`.
// All of that came from one decision — building in the tree the user
// works in.
//
// Now the channel keeps a tree of its own: a linked worktree of the
// checkout under the state dir, detached at the pinned remote's main.
// It shares the checkout's object store, so it costs no second clone,
// and it is the only tree this code moves. The checkout is consulted
// for exactly two things — which remote is the pinned repository, and
// its object store — and is otherwise left as the user had it.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/registry"
)

// latestBuildTree is the worktree the latest channel checks out and
// builds in. Under the state dir so it inherits HIVE_STATE_DIR
// isolation, and beside — not under — updatesRoot, which
// pruneStagingDirs removes wholesale after every install: the tree's
// node_modules is the one thing worth keeping between updates.
func latestBuildTree() string {
	return filepath.Join(registry.StateDir(), "latest-src")
}

// prepareBuildTree fetches the pinned remote through the checkout and
// leaves the build tree checked out at that remote's main. It returns
// the tree.
//
// The first call adds the tree as a linked worktree; every later call
// re-checks it out in place, so the frontend dependencies build.sh
// installs there are paid for once. Both run with the checkout's hooks
// disabled: `worktree add` and `checkout` fire post-checkout before
// build.sh gets a turn, and a planted hook must not execute from a
// button press. Nothing here needs hooks.
//
// The fetch comes first and a failure stops everything: the tree would
// otherwise be moved to whatever was fetched last and built as an
// "update" to something the user already has.
func prepareBuildTree(repo, remote string) (string, error) {
	if _, err := runGitFn(repo, "fetch", "--quiet", remote); err != nil {
		return "", fmt.Errorf("couldn't fetch %s in %s — check your network or credentials and update again: %w",
			remote, repo, err)
	}
	tree := latestBuildTree()
	ref := remote + "/" + latestBranch

	if isLinkedWorktree(tree) && ownsWorktree(repo, tree) {
		// --force: the tree is this code's own, so anything a previous
		// build left changed in it is discarded rather than allowed to
		// block the checkout. Ignored files — node_modules, build/bin —
		// survive, which is the point of reusing the tree.
		if _, err := runGitFn(tree, "-c", "core.hooksPath=/dev/null", "checkout", "--quiet", "--detach", "--force", ref); err != nil {
			return "", err
		}
		return tree, nil
	}

	// Anything else at that path — an add that died half way, a .git
	// deleted by hand, a worktree of some other clone that source_repo
	// used to point at — would either make `worktree add` refuse or
	// build the wrong repository. It is the updater's private
	// directory, so it is replaced, and a registration git still holds
	// for it is pruned so the add is not refused for that either.
	if err := os.RemoveAll(tree); err != nil {
		return "", fmt.Errorf("clear %s: %w", tree, err)
	}
	if _, err := runGitFn(repo, "worktree", "prune"); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(tree), 0o755); err != nil {
		return "", err
	}
	if _, err := runGitFn(repo, "-c", "core.hooksPath=/dev/null", "worktree", "add", "--detach", tree, ref); err != nil {
		return "", err
	}
	return tree, nil
}

// isLinkedWorktree reports whether dir is a linked git worktree: one
// whose .git is a file pointing back at the main repository, not a
// directory of its own.
func isLinkedWorktree(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && !st.IsDir()
}

// ownsWorktree reports whether repo lists tree among its worktrees —
// whether the tree is a worktree of *this* checkout rather than of
// some other clone. Asked of git rather than read out of the tree's
// .git file, so git's own path spelling is what gets compared.
func ownsWorktree(repo, tree string) bool {
	out, err := runGitFn(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	want := comparablePath(tree)
	for _, line := range strings.Split(out, "\n") {
		path, ok := strings.CutPrefix(line, "worktree ")
		if ok && comparablePath(path) == want {
			return true
		}
	}
	return false
}

// comparablePath reduces a directory path to a form two spellings of
// the same directory share: symlinks resolved where the path exists,
// separators unified, case folded. Case folding is right on the two
// platforms with an updater (both default to case-insensitive
// filesystems) and harmless on the third, where this is a worktree
// registration check, not a security boundary.
func comparablePath(p string) string {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return strings.ToLower(filepath.ToSlash(p))
}
