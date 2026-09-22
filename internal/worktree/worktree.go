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
	"strconv"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/proc"
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
// Bounded like every other read here: a repo on a stalled network
// mount would otherwise hang whoever asked, and the daemon's
// orphan-worktree scan asks once per project at boot.
func Root(dir string) (string, error) {
	out, err := git(context.Background(), readTimeout, dir, "rev-parse", "--show-toplevel")
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

// FetchError reports that the pre-branch `git fetch origin` failed, so
// the cached `origin/HEAD` this repo already has may be behind the real
// remote. It carries what a caller needs to ask the user an informed
// question: git's own stderr, the ref that would be used anyway, and
// how old that ref's tip is.
//
// It exists because branching from a stale ref silently is the bug
// this type was introduced to kill — see
// docs/product-specs/451-worktree-setup-failure-prompt.md. Callers that
// genuinely cannot ask (no client attached) must fail, not fall back.
type FetchError struct {
	// BaseRef is the cached upstream ref (e.g. "origin/main"), or ""
	// when origin/HEAD could not be resolved at all.
	BaseRef string
	// CachedTip is the commit BaseRef points at, or "" when unresolved.
	CachedTip string
	// TipAge is how long ago CachedTip was committed. Zero when unknown.
	TipAge time.Duration
	// Stderr is git's own message, already trimmed.
	Stderr string
	Err    error
}

func (e *FetchError) Error() string {
	return fmt.Sprintf("git fetch origin: %s: %v", e.Stderr, e.Err)
}

func (e *FetchError) Unwrap() error { return e.Err }

// PrepareBase resolves the ref a new branch should be created from,
// refreshing `origin` first so it reflects the latest remote state.
//
// Returns ("", nil) when the repo has no `origin` remote: there is no
// upstream to be stale against, so branching from HEAD is correct and
// there is nothing to ask the user about.
//
// Returns a *FetchError when the fetch failed. The cached ref is still
// reported in the error, so a caller that asks the user can offer it as
// an explicit choice — but PrepareBase never makes that choice itself.
func PrepareBase(ctx context.Context, repoDir string) (string, error) {
	// Confirm `origin` exists before spending time on a fetch.
	checkCtx, checkCancel := context.WithTimeout(ctx, 3*time.Second)
	defer checkCancel()
	if err := proc.CommandContext(checkCtx, "git", "-C", repoDir, "remote", "get-url", "origin").Run(); err != nil {
		return "", nil
	}

	fetchCtx, fetchCancel := context.WithTimeout(ctx, 10*time.Second)
	defer fetchCancel()
	fetchOut, fetchErr := proc.CommandContext(fetchCtx, "git", "-C", repoDir, "fetch", "--quiet", "origin").CombinedOutput()
	if fetchErr != nil {
		// Resolve the cached ref anyway: it is what the user will be
		// offered as the explicit fallback, and the age is what makes
		// that offer meaningful.
		base := resolveOriginHead(ctx, repoDir)
		tip, age := tipAndAge(ctx, repoDir, base)
		return "", &FetchError{
			BaseRef:   base,
			CachedTip: tip,
			TipAge:    age,
			// git echoes the remote back, and an HTTPS remote can carry a
			// token in its userinfo. This text reaches a GUI dialog and
			// hived.log, so scrub it on the way out — same helper the
			// worktree inventory already uses for the same reason.
			Stderr: scrubURLCredentials(strings.TrimSpace(string(fetchOut))),
			Err:    fetchErr,
		}
	}

	base := resolveOriginHead(ctx, repoDir)
	if base == "" {
		log.Printf("worktree: origin/HEAD not set in %s; new branch will come from local HEAD", repoDir)
	}
	return base, nil
}

// resolveOriginHead resolves origin/HEAD -> origin/<default-branch>,
// or "" when it is not set.
func resolveOriginHead(ctx context.Context, repoDir string) string {
	resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := proc.CommandContext(resolveCtx, "git", "-C", repoDir,
		"symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tipAndAge returns the commit ref points at and how long ago it was
// committed. Both are best-effort: a repo with no such ref reports
// ("", 0), which callers render as "unknown".
func tipAndAge(ctx context.Context, repoDir, ref string) (string, time.Duration) {
	if ref == "" {
		return "", 0
	}
	out, err := git(ctx, readTimeout, repoDir, "log", "-1", "--format=%H %ct", ref)
	if err != nil {
		return "", 0
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return "", 0
	}
	secs, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return fields[0], 0
	}
	age := time.Since(time.Unix(secs, 0))
	if age < 0 {
		age = 0
	}
	return fields[0], age
}

// CreateWorktree resolves the base ref itself and then creates the
// worktree. It is the non-interactive entry point: callers that want
// to ask the user what to do about a failed fetch call PrepareBase and
// CreateWorktreeAt separately (see internal/registry).
//
// Note it does NOT fall back to local HEAD when an add against a
// resolved base ref fails — that fallback silently produced
// wrong-base worktrees. The error propagates instead.
func CreateWorktree(ctx context.Context, repoDir, branch, worktreePath string) error {
	// Checking out a branch that already exists never consults
	// upstream, so it must not pay the fetch's latency. Probed HERE,
	// in the shared function, rather than in one caller: the registry's
	// create path had this restored on its own, while reopening a
	// closed session (closed.go) and the worktree browser
	// (worktrees.go) still paid a 10s fetch they then ignored — the
	// browser one while holding gitMu, blocking every other create and
	// kill. Fixing the shared function covers all three.
	if branchExists(ctx, repoDir, branch) {
		return CreateWorktreeAt(ctx, repoDir, branch, worktreePath, "")
	}

	base, err := PrepareBase(ctx, repoDir)
	if err != nil {
		var fe *FetchError
		if errors.As(err, &fe) {
			// Non-interactive caller: keep today's behaviour of using
			// the cached ref, but say so loudly.
			log.Printf("worktree: %v; branching from cached %s", fe, fe.BaseRef)
			base = fe.BaseRef
		} else {
			return err
		}
	}
	return CreateWorktreeAt(ctx, repoDir, branch, worktreePath, base)
}

// CreateWorktreeAt runs `git worktree add` for the given branch,
// creating it from base when the branch does not exist yet. An empty
// base means "from local HEAD", which is correct only when the repo has
// no upstream — never as a silent fallback for a failed fetch.
//
// Bounded by a 30-second timeout so a slow / hung filesystem can't lock
// up session creation forever, and by ctx so daemon shutdown cancels
// in-flight git work.
func CreateWorktreeAt(ctx context.Context, repoDir, branch, worktreePath, base string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Existing branch? Probe the ref directly — exit-code based, so it
	// works regardless of git's message locale. (The substring check
	// below stays only as a TOCTOU safety net for a branch created
	// between this probe and the add.) Checking out an existing branch
	// never consults upstream, so base is irrelevant here.
	if branchExists(ctx, repoDir, branch) {
		out, err := gitWorktreeAdd(ctx, repoDir, worktreePath, branch)
		if err != nil {
			return fmt.Errorf("git worktree add (existing branch %s): %s: %w",
				branch, strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	var attempts []error

	args := []string{"-b", branch, worktreePath}
	if base != "" {
		args = append(args, base)
	}
	out, err := gitWorktreeAdd(ctx, repoDir, args...)
	if err == nil {
		return nil
	}
	attempts = append(attempts, fmt.Errorf("new branch %s (base %q): %s: %w",
		branch, base, strings.TrimSpace(string(out)), err))

	// The branch appeared between the probe above and the add (TOCTOU):
	// fall back to checking it out. That is the same branch, not a
	// different base, so it is not a silent-fallback hazard.
	// gitWorktreeAdd pins LC_ALL=C, so these substrings are stable
	// across user locales.
	if strings.Contains(string(out), "already exists") || strings.Contains(string(out), "fatal: A branch named") {
		out2, err2 := gitWorktreeAdd(ctx, repoDir, worktreePath, branch)
		if err2 == nil {
			return nil
		}
		attempts = append(attempts, fmt.Errorf("existing branch %s: %s: %w",
			branch, strings.TrimSpace(string(out2)), err2))
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
	cmd := proc.CommandContext(ctx, "git", full...)
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
	return proc.CommandContext(ctx, "git", "-C", repoDir,
		"rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
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
	removeOut, removeErr := git(context.Background(), mutateTimeout, repoDir,
		"worktree", "remove", "--force", worktreePath)
	// Always try the FS removal too — `git worktree remove` may have
	// succeeded but left a stray dir, or it may have skipped it.
	_ = os.RemoveAll(worktreePath)
	// Prune git's admin state regardless of how the above went.
	if out, err := git(context.Background(), mutateTimeout, repoDir, "worktree", "prune"); err != nil {
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
//
// ponytail: only symlinks are excluded, so the copy fallback (Windows
// without symlink privileges) still reads as dirty. Distinguishing a
// fallback copy from the user's own file needs recorded provenance; a
// spurious prompt there beats deleting real work.
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
			rel := dir + "/" + name
			// Only exclude what is still a symlink. Excluding these paths
			// by name would hide real work: a project that COMMITS
			// .claude/commands sees LinkAgentConfig skip it (destination
			// exists), so the path is not ours at all, and an edit to it
			// must still count. Same if the user replaces a link with real
			// local content.
			fi, err := os.Lstat(filepath.Join(worktreePath, rel))
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			args = append(args, ":(exclude)"+rel)
		}
	}
	// Bounded like the rest of the read path: this runs once per
	// candidate in the daemon's orphan-worktree scan, and a `git
	// status` on a wedged repo must not hang it.
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return false, fmt.Errorf("git status: timed out after %s", readTimeout)
		}
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
	cmd := proc.Command("git", "-C", repoRoot, "check-ignore", "-q", ".worktrees")
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
