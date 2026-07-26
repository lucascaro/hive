// Package worktree manages git worktrees on behalf of Hive sessions.
//
// A worktree-backed session lives inside <gitRoot>/.worktrees/<branch>
// instead of the project's main checkout, so multiple agents can run
// in parallel against the same repo without stepping on each other's
// uncommitted changes. This package owns the create / remove / probe
// path; lifecycle integration (when to create, when to clean up) lives
// in internal/registry.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsGitRepo reports whether dir (or any of its parents) is inside a
// git repository.
func IsGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := Root(dir)
	return err == nil
}

// Root returns the absolute path of the git repository root that
// contains dir.
func Root(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WorktreePath returns the on-disk path for a worktree backing the
// given branch. Worktrees live at <gitRoot>/.worktrees/<sanitized>.
func WorktreePath(gitRoot, branch string) string {
	return filepath.Join(gitRoot, ".worktrees", sanitizeBranch(branch))
}

// CreateWorktree runs `git worktree add` for the given branch. If the
// branch doesn't exist yet, it is created from the detected upstream
// default ref (typically `origin/main`) so worktrees start on the
// latest upstream tip even when the local default branch is stale.
// When no upstream is configured or the remote is unreachable, falls
// back to creating the branch from local HEAD. Bounded by a 30-second
// timeout so a slow / hung filesystem can't lock up session creation
// forever, and by ctx so daemon shutdown cancels in-flight git work.
func CreateWorktree(ctx context.Context, repoDir, branch, worktreePath string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Existing branch? Probe the ref directly — exit-code based, so it
	// works regardless of git's message locale. (The substring check
	// below stays only as a TOCTOU safety net for a branch created
	// between this probe and the add.) Probed before upstreamBaseRef:
	// checking out an existing branch never branches from upstream, so
	// it must not pay the fetch's network latency.
	if branchExists(ctx, repoDir, branch) {
		out, err := gitWorktreeAdd(ctx, repoDir, worktreePath, branch)
		if err != nil {
			return fmt.Errorf("git worktree add (existing branch %s): %s: %w",
				branch, strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	// Best-effort: refresh `origin` so the upstream tip we branch from
	// reflects the latest remote state. Failures are logged via the
	// returned base ref staying empty (callers fall back to HEAD).
	upstream := upstreamBaseRef(ctx, repoDir)

	var attempts []error

	// Attempt 1: new branch from the upstream tip (or HEAD when no
	// upstream resolved).
	args := []string{"-b", branch, worktreePath}
	if upstream != "" {
		args = append(args, upstream)
	}
	out, err := gitWorktreeAdd(ctx, repoDir, args...)
	if err == nil {
		return nil
	}
	attempts = append(attempts, fmt.Errorf("new branch %s (base %q): %s: %w",
		branch, upstream, strings.TrimSpace(string(out)), err))

	// The branch appeared between the probe above and the add (TOCTOU):
	// fall back to checking it out. gitWorktreeAdd pins LC_ALL=C, so
	// these substrings are stable across user locales.
	if strings.Contains(string(out), "already exists") || strings.Contains(string(out), "fatal: A branch named") {
		out2, err2 := gitWorktreeAdd(ctx, repoDir, worktreePath, branch)
		if err2 == nil {
			return nil
		}
		attempts = append(attempts, fmt.Errorf("existing branch %s: %s: %w",
			branch, strings.TrimSpace(string(out2)), err2))
		return fmt.Errorf("git worktree add: %w", errors.Join(attempts...))
	}

	// If we asked for an upstream base ref and it failed for some other
	// reason (e.g. ref disappeared between fetch and add), retry without
	// the explicit base ref so HEAD is used. This keeps creation robust
	// in offline / shallow / sandboxed environments.
	if upstream != "" {
		out3, err3 := gitWorktreeAdd(ctx, repoDir, "-b", branch, worktreePath)
		if err3 == nil {
			return nil
		}
		attempts = append(attempts, fmt.Errorf("new branch %s (no base): %s: %w",
			branch, strings.TrimSpace(string(out3)), err3))
	}
	return fmt.Errorf("git worktree add: %w", errors.Join(attempts...))
}

// gitWorktreeAdd runs `git -C repoDir worktree add <args…>` with a 30s
// timeout and a C locale. The locale pin keeps CreateWorktree's
// error-text fallback meaningful on non-English systems.
func gitWorktreeAdd(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	full := append([]string{"-C", repoDir, "worktree", "add"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("timed out after 30s")
	}
	return out, err
}

// branchExists reports whether refs/heads/<branch> exists in repoDir.
func branchExists(ctx context.Context, repoDir, branch string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "git", "-C", repoDir,
		"rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// upstreamBaseRef returns the short name of the upstream default ref
// (e.g. `origin/main`) when one is configured, or "" otherwise. Before
// resolving, it best-effort fetches `origin` so the returned ref points
// at the latest remote tip. Bounded by short timeouts; never blocks
// worktree creation for long.
func upstreamBaseRef(ctx context.Context, repoDir string) string {
	// Confirm `origin` exists before spending time on a fetch.
	checkCtx, checkCancel := context.WithTimeout(ctx, 3*time.Second)
	defer checkCancel()
	if err := exec.CommandContext(checkCtx, "git", "-C", repoDir, "remote", "get-url", "origin").Run(); err != nil {
		return ""
	}

	// Best-effort fetch (10s). Network or auth failures fall through:
	// we'll still resolve whatever `origin/HEAD` already points at locally.
	// Warn so a stale cached origin/HEAD doesn't silently base new
	// worktrees on outdated upstream — the very failure mode #192 fixed.
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 10*time.Second)
	defer fetchCancel()
	if fetchOut, fetchErr := exec.CommandContext(fetchCtx, "git", "-C", repoDir, "fetch", "--quiet", "origin").CombinedOutput(); fetchErr != nil {
		log.Printf("worktree: fetch origin failed (%v); new worktree may be based on stale origin/HEAD: %s", fetchErr, strings.TrimSpace(string(fetchOut)))
	}

	// Resolve origin/HEAD -> origin/<default-branch>.
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 3*time.Second)
	defer resolveCancel()
	out, err := exec.CommandContext(resolveCtx, "git", "-C", repoDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		log.Printf("worktree: origin/HEAD not set in %s; falling back to local HEAD for new worktree", repoDir)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Cleanup is the v2 idempotent removal helper used by registry.Kill
// and the daemon-startup orphan reclaim. Runs `git worktree remove
// --force`, then `os.RemoveAll`, then `git worktree prune` to clean
// stale admin entries even when the dir was deleted out-from-under us.
// Tolerates a missing dir / missing repo.
func Cleanup(repoDir, worktreePath string) error {
	if worktreePath == "" {
		return errors.New("worktree.Cleanup: empty path")
	}
	// Best-effort `worktree remove --force`. If the dir is missing,
	// git may exit non-zero — that's fine; we just want to make sure
	// the registered worktree (if any) is gone.
	removeCmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", worktreePath)
	removeOut, removeErr := removeCmd.CombinedOutput()
	// Always try the FS removal too — `git worktree remove` may have
	// succeeded but left a stray dir, or it may have skipped it.
	_ = os.RemoveAll(worktreePath)
	// Prune git's admin state regardless of how the above went.
	pruneCmd := exec.Command("git", "-C", repoDir, "worktree", "prune")
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune: %s", strings.TrimSpace(string(out)))
	}
	if removeErr != nil {
		// Surface the remove error but don't escalate — pruning
		// already cleaned admin state, and the dir is gone.
		return fmt.Errorf("git worktree remove: %s (best-effort completed)", strings.TrimSpace(string(removeOut)))
	}
	return nil
}

// agentConfigDirs are the per-project agent config directories that
// live in the main checkout but are typically untracked, so a fresh
// `git worktree add` leaves them behind — taking the project's skills,
// commands and local settings with them.
var agentConfigDirs = []string{".claude", ".agents"}

// agentConfigEntries is the allowlist of children linked when a config
// dir already exists in the worktree. Deliberately an allowlist, not a
// denylist: these dirs also hold per-run *state* (lock files, task
// queues) which must stay private to each checkout, and the cost of
// missing an entry (a skill doesn't show up) is far below the cost of
// wrongly sharing state between worktrees.
var agentConfigEntries = []string{
	"agents", "commands", "hooks", "output-styles", "plugins", "skills",
	"settings.local.json",
}

// LinkAgentConfig symlinks the repo's agent config (.claude, .agents)
// into a freshly created worktree so sessions there see the same
// skills, commands and local settings as the main checkout.
//
// Symlinks (not copies) so that a skill added or edited in the main
// checkout is immediately visible from every existing worktree.
//
// Never clobbers: a destination path that already exists is skipped,
// which is what keeps tracked files (e.g. a committed
// .claude/settings.json that `git worktree add` already checked out)
// untouched. Links are per-entry (see agentConfigEntries) rather than
// whole-dir so per-checkout state in the same dirs stays unshared.
// Best-effort — problems are logged, never returned.
func LinkAgentConfig(repoRoot, worktreePath string) {
	if repoRoot == "" || worktreePath == "" || repoRoot == worktreePath {
		return
	}
	for _, dir := range agentConfigDirs {
		srcDir := filepath.Join(repoRoot, dir)
		if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
			continue
		}
		dstDir := filepath.Join(worktreePath, dir)
		for _, name := range agentConfigEntries {
			src := filepath.Join(srcDir, name)
			if _, err := os.Lstat(src); err != nil {
				// Absence is the normal case (most projects have only a
				// couple of these); anything else means we're skipping
				// config the user does have, so say so.
				if !errors.Is(err, fs.ErrNotExist) {
					log.Printf("worktree: stat %s: %v", src, err)
				}
				continue
			}
			dst := filepath.Join(dstDir, name)
			if _, err := os.Lstat(dst); err == nil {
				continue // exists — leave it alone
			}
			if err := os.MkdirAll(dstDir, 0o755); err != nil {
				log.Printf("worktree: mkdir %s: %v", dstDir, err)
				break
			}
			if err := os.Symlink(src, dst); err != nil {
				// Windows needs Developer Mode or elevation to create a
				// symlink, and without a fallback the whole feature is a
				// no-op there. A copy loses the live-edit property — a
				// skill changed in the main checkout won't reach an
				// existing worktree — but stale config beats none.
				if cerr := copyEntry(src, dst); cerr != nil {
					log.Printf("worktree: link %s: %v (copy fallback: %v)",
						filepath.Join(dir, name), err, cerr)
				}
			}
		}
	}
}

// copyEntry copies src to dst, recursing when src is a directory. Used
// only as LinkAgentConfig's fallback when symlinking is unavailable, so
// dst is always known-absent by the time we get here.
func copyEntry(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return os.CopyFS(dst, os.DirFS(src))
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, st.Mode().Perm())
}

// HasUncommitted reports whether the worktree has tracked changes,
// untracked files, or staged-but-uncommitted changes. Returns
// (false, nil) when worktreePath is missing — a missing worktree
// can't have uncommitted work to lose, so the caller should proceed.
//
// The agent config LinkAgentConfig planted is excluded: those entries
// are hive's own doing, not the user's uncommitted work, and counting
// them would make every pristine worktree refuse to close (see
// registry.ErrWorktreeDirty). Projects that gitignore their agent
// config never hit that; this covers the ones that don't.
func HasUncommitted(worktreePath string) (bool, error) {
	if _, err := os.Stat(worktreePath); err != nil {
		return false, nil
	}
	// -uall is load-bearing: without it git collapses an untracked
	// directory to a single "?? .claude/" entry and never descends far
	// enough for the pathspec exclusions below to match.
	args := []string{"-C", worktreePath, "status", "--porcelain", "-uall", "--", "."}
	for _, dir := range agentConfigDirs {
		for _, name := range agentConfigEntries {
			args = append(args, ":(exclude)"+dir+"/"+name)
		}
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status: %s", strings.TrimSpace(string(out)))
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// AddToGitignore appends pattern as a new line to <gitRoot>/.gitignore,
// creating the file if it does not exist.
func AddToGitignore(gitRoot, pattern string) error {
	path := filepath.Join(gitRoot, ".gitignore")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# hive worktrees\n%s\n", pattern)
	return err
}

// EnsureGitignore appends ".worktrees" to <root>/.gitignore iff the
// file already exists AND `git check-ignore` says .worktrees isn't
// already covered. Best-effort; never errors.
//
// Rationale: we don't want to create a .gitignore from scratch (the
// user may genuinely not want one), but for the common case of a
// repo that already has a .gitignore we silently keep .worktrees out
// of git history. `git check-ignore` is consulted so that global
// excludes (e.g. ~/.gitignore_global) and ancestor .gitignore files
// are respected too.
func EnsureGitignore(repoRoot string) {
	if repoRoot == "" {
		return
	}
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	if _, err := os.Stat(gitignorePath); err != nil {
		return // no .gitignore present; do not create one
	}
	// `git check-ignore -q .worktrees` exits 0 when matched, 1 when
	// not matched, >1 on error. We only want to add when not matched.
	cmd := exec.Command("git", "-C", repoRoot, "check-ignore", "-q", ".worktrees")
	err := cmd.Run()
	if err == nil {
		return // already covered
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		_ = AddToGitignore(repoRoot, ".worktrees")
	}
	// Any other error: leave the file alone.
}

// ResolveBranchAndPath produces a (branch, path) pair safe to hand to
// CreateWorktree. If requested is empty, a random adjective-noun is
// generated. If <root>/.worktrees/<sanitized> is already a directory,
// suffixes -2, -3, … on both the branch name and the path until an
// unused slot is found.
func ResolveBranchAndPath(repoRoot, requested string) (branch, path string, err error) {
	if repoRoot == "" {
		return "", "", errors.New("worktree.ResolveBranchAndPath: empty repo root")
	}
	base := requested
	if base == "" {
		base = RandomBranchName()
	}
	candidate := base
	for suffix := 2; suffix < 100; suffix++ {
		path = WorktreePath(repoRoot, candidate)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return candidate, path, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
	return "", "", fmt.Errorf("worktree.ResolveBranchAndPath: too many collisions for %q", base)
}

// RandomBranchName returns a random "adjective-noun" branch name.
func RandomBranchName() string {
	adj := adjectives[randIndex(len(adjectives))]
	noun := nouns[randIndex(len(nouns))]
	return adj + "-" + noun
}

func randIndex(n int) int {
	if n <= 0 {
		return 0
	}
	return rand.IntN(n)
}

// sanitizeBranch replaces characters that are invalid in directory
// names with '-'. Same set as v1.
func sanitizeBranch(branch string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	return r.Replace(branch)
}

var adjectives = []string{
	"amber", "ancient", "arctic", "autumn", "azure",
	"bold", "brave", "bright", "brisk", "broad",
	"calm", "cedar", "clear", "crisp", "curly",
	"daring", "dark", "dawn", "deep", "distant",
	"eager", "early", "echo", "elder", "ember",
	"faint", "feral", "fierce", "firm", "fleet",
	"gentle", "gilded", "golden", "grand", "green",
	"hidden", "hollow", "humble", "hushed", "icy",
	"jade", "keen", "kind", "lofty", "lone",
	"lunar", "misty", "noble", "north", "oak",
	"pale", "proud", "pure", "quick", "quiet",
	"rapid", "raven", "red", "rich", "rising",
	"rough", "royal", "rustic", "sandy", "serene",
	"sharp", "silent", "silver", "sleek", "slim",
	"slow", "small", "solar", "solid", "stone",
	"storm", "strong", "sunny", "swift", "tall",
	"tawny", "thin", "tidal", "timber", "tiny",
	"true", "twilight", "vast", "warm", "white",
	"wild", "windy", "winter", "wise", "young",
}

var nouns = []string{
	"anchor", "arc", "arrow", "ash", "atlas",
	"bay", "beam", "bear", "birch", "blade",
	"bloom", "boat", "brook", "brush", "canyon",
	"cedar", "cliff", "cloud", "coast", "comet",
	"cove", "creek", "crest", "crow", "crystal",
	"dawn", "delta", "dune", "dusk", "dust",
	"eagle", "echo", "elm", "ember", "fern",
	"field", "flint", "forest", "forge", "fox",
	"frost", "gale", "gate", "glade", "glen",
	"grove", "gust", "harbor", "haze", "heath",
	"helm", "hill", "hollow", "horizon", "isle",
	"jade", "lake", "lark", "leaf", "light",
	"log", "marsh", "mast", "mesa", "mist",
	"moon", "moss", "mountain", "oak", "ocean",
	"path", "peak", "pine", "plain", "pond",
	"prism", "rain", "reef", "ridge", "river",
	"rock", "root", "sage", "sand", "sea",
	"shell", "shore", "sky", "slope", "snow",
	"spark", "spire", "star", "stone", "storm",
	"stream", "summit", "sun", "tide", "timber",
	"trail", "vale", "valley", "wave", "wind",
}
