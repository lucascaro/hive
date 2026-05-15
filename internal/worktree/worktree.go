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
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
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
// forever.
func CreateWorktree(repoDir, branch, worktreePath string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Best-effort: refresh `origin` so the upstream tip we branch from
	// reflects the latest remote state. Failures are logged via the
	// returned base ref staying empty (callers fall back to HEAD).
	upstream := upstreamBaseRef(repoDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"-C", repoDir, "worktree", "add", "-b", branch, worktreePath}
	if upstream != "" {
		args = append(args, upstream)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("git worktree add timed out after 30s")
	}
	// Fall back to checking out an existing branch.
	if strings.Contains(string(out), "already exists") || strings.Contains(string(out), "fatal: A branch named") {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel2()
		cmd2 := exec.CommandContext(ctx2, "git", "-C", repoDir, "worktree", "add", worktreePath, branch)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out2)))
		}
		return nil
	}
	// If we asked for an upstream base ref and it failed for some other
	// reason (e.g. ref disappeared between fetch and add), retry without
	// the explicit base ref so HEAD is used. This keeps creation robust
	// in offline / shallow / sandboxed environments.
	if upstream != "" {
		ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel3()
		cmd3 := exec.CommandContext(ctx3, "git", "-C", repoDir, "worktree", "add", "-b", branch, worktreePath)
		if out3, err3 := cmd3.CombinedOutput(); err3 == nil {
			return nil
		} else {
			out, err = out3, err3
		}
	}
	return fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out)))
}

// upstreamBaseRef returns the short name of the upstream default ref
// (e.g. `origin/main`) when one is configured, or "" otherwise. Before
// resolving, it best-effort fetches `origin` so the returned ref points
// at the latest remote tip. Bounded by short timeouts; never blocks
// worktree creation for long.
func upstreamBaseRef(repoDir string) string {
	// Confirm `origin` exists before spending time on a fetch.
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer checkCancel()
	if err := exec.CommandContext(checkCtx, "git", "-C", repoDir, "remote", "get-url", "origin").Run(); err != nil {
		return ""
	}

	// Best-effort fetch (10s). Network or auth failures fall through:
	// we'll still resolve whatever `origin/HEAD` already points at locally.
	// Warn so a stale cached origin/HEAD doesn't silently base new
	// worktrees on outdated upstream — the very failure mode #192 fixed.
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer fetchCancel()
	if fetchOut, fetchErr := exec.CommandContext(fetchCtx, "git", "-C", repoDir, "fetch", "--quiet", "origin").CombinedOutput(); fetchErr != nil {
		log.Printf("worktree: fetch origin failed (%v); new worktree may be based on stale origin/HEAD: %s", fetchErr, strings.TrimSpace(string(fetchOut)))
	}

	// Resolve origin/HEAD -> origin/<default-branch>.
	resolveCtx, resolveCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer resolveCancel()
	out, err := exec.CommandContext(resolveCtx, "git", "-C", repoDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		log.Printf("worktree: origin/HEAD not set in %s; falling back to local HEAD for new worktree", repoDir)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RemoveWorktree runs `git worktree remove --force` and then deletes
// the dir if git left it behind.
func RemoveWorktree(repoDir, worktreePath string) error {
	cmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", worktreePath)
	out, err := cmd.CombinedOutput()
	_ = os.RemoveAll(worktreePath)
	if err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}
	return nil
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

// HasUncommitted reports whether the worktree has tracked changes,
// untracked files, or staged-but-uncommitted changes. Returns
// (false, nil) when worktreePath is missing — a missing worktree
// can't have uncommitted work to lose, so the caller should proceed.
func HasUncommitted(worktreePath string) (bool, error) {
	if _, err := os.Stat(worktreePath); err != nil {
		return false, nil
	}
	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status: %s", strings.TrimSpace(string(out)))
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// IsInGitignore reports whether pattern already appears (as its own
// line) in the .gitignore file at gitRoot. Returns false if the file
// does not exist.
func IsInGitignore(gitRoot, pattern string) bool {
	path := filepath.Join(gitRoot, ".gitignore")
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == pattern {
			return true
		}
	}
	return false
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
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int(binary.BigEndian.Uint64(b[:]) % uint64(n))
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
