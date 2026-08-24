package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Read-path timeout. Every function here shells out to git for
// inspection only, so a hung filesystem must not stall the caller for
// long. Mutations reuse the package's 30s bound (see mutateTimeout).
const (
	readTimeout   = 5 * time.Second
	mutateTimeout = 30 * time.Second
)

// git runs `git -C dir <args…>` with a bounded timeout and a C locale.
// The locale pin keeps error-text matching meaningful on non-English
// systems, the same reason gitWorktreeAdd pins it.
func git(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git %s: timed out after %s", args[0], timeout)
	}
	if err != nil {
		return out, fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(string(out)), err)
	}
	return out, nil
}

// Info describes one entry of `git worktree list`. The main checkout is
// included by git and by this function; callers that only care about
// hive-managed worktrees filter on the path prefix themselves.
type Info struct {
	Path     string // absolute, symlinks resolved
	Branch   string // short name; "" when detached
	Head     string // commit sha
	Detached bool
	Locked   bool
	Prunable bool
}

// List returns every worktree registered in repoRoot, main checkout
// first (git's own ordering).
func List(repoRoot string) ([]Info, error) {
	out, err := git(context.Background(), readTimeout, repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(string(out)), nil
}

// parseWorktreeList decodes `git worktree list --porcelain`. Records
// are separated by a blank line; each starts with a `worktree <path>`
// line. Split out from List so the parser is testable without a repo.
func parseWorktreeList(out string) []Info {
	var list []Info
	var cur *Info
	flush := func() {
		if cur != nil {
			list = append(list, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			flush()
			cur = &Info{Path: ResolvePath(val)}
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	flush()
	return list
}

// ResolvePath normalizes a path for comparison against registry-held
// paths. Exported because the registry compares Entry.WorktreePath
// against what List reports and the two must agree. macOS reports /private/var where callers hold /var (and vice
// versa), so every path that crosses this package's boundary goes
// through EvalSymlinks. Falls back to the input when the path is gone.
func ResolvePath(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	// The path no longer exists — a stale worktree entry git has not
	// pruned yet, which is exactly the kind the browser needs to clean
	// up. EvalSymlinks fails outright on a missing leaf, so resolve the
	// nearest existing ancestor and re-append the missing tail.
	// Without this, a deleted worktree under /var resolves to /var
	// while its repo root resolves to /private/var, and the registry's
	// containment check then refuses to remove it (macOS only, which is
	// how it would have escaped notice).
	dir, base := filepath.Split(p)
	dir = filepath.Clean(dir)
	if base == "" || dir == p {
		return p
	}
	return filepath.Join(ResolvePath(dir), base)
}

// ManagedDir is where hive keeps the worktrees it owns. Anything
// outside it belongs to the user — their main checkout, or a worktree
// they made themselves — and hive must never delete it.
func ManagedDir(root string) string {
	return filepath.Join(root, ".worktrees")
}

// IsManaged reports whether path is a worktree hive owns: a direct
// entry under <root>/.worktrees. This is the single definition of that
// boundary; the registry's removal, rename and adoption paths all ask
// here rather than each rolling their own prefix check.
//
// filepath.Rel rather than a string prefix: a prefix test accepts
// "<root>/.worktrees-evil" and cannot see through "..". Both paths are
// symlink-resolved first so a /var vs /private/var mismatch (macOS)
// does not read as an escape.
func IsManaged(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	base := ResolvePath(ManagedDir(root))
	rel, err := filepath.Rel(base, ResolvePath(path))
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// MainRoot returns the main checkout's root for the repository
// containing dir — even when dir is itself a linked worktree, where
// Root reports that worktree's own top level rather than the repo's.
// Callers that need to reason about the repo as a whole (which
// directory is "the" checkout, where .worktrees/ lives) must use this,
// not Root.
func MainRoot(dir string) (string, error) {
	out, err := git(context.Background(), readTimeout, dir,
		"rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		// --path-format predates git 2.31; fall back to the plain
		// top-level, which is correct whenever dir is the main
		// checkout (the common case).
		return Root(dir)
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return Root(dir)
	}
	return ResolvePath(filepath.Dir(common)), nil
}

// BranchInfo describes one local branch. Ahead counts commits not
// reachable from the branch's comparison base (upstream when set,
// otherwise the repo's default ref) — the same base Status.Unpushed
// uses, so the two agree about what "unpushed work" means.
type BranchInfo struct {
	Name        string
	Upstream    string // "" = no upstream configured
	Ahead       int
	HasWorktree bool
	Merged      bool // reachable from the default ref
}

// ListBranches returns every local branch with worktree and
// merged-ness annotations. Never fetches: this is a read path that
// must not block on the network.
func ListBranches(repoRoot string) ([]BranchInfo, error) {
	// %(worktreepath) is empty for branches with no worktree; the
	// separator is \x00 so branch names containing spaces survive.
	const format = "%(refname:short)%00%(upstream:short)%00%(worktreepath)"
	out, err := git(context.Background(), readTimeout, repoRoot,
		"for-each-ref", "--format="+format, "refs/heads")
	if err != nil {
		return nil, err
	}
	base := defaultRef(repoRoot)
	merged := mergedBranches(repoRoot, base)

	var list []BranchInfo
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		b := BranchInfo{
			Name:        parts[0],
			Upstream:    parts[1],
			HasWorktree: parts[2] != "",
			Merged:      merged[parts[0]],
		}
		// Display only: an unanswerable count shows as 0 ahead here. The
		// deletion decision does not come from this list — it comes from
		// Inspect, which treats the same failure as Unknown.
		b.Ahead, _ = aheadCount(repoRoot, b.Name, comparisonBase(b.Upstream, base))
		list = append(list, b)
	}
	return list, nil
}

// defaultRef returns the ref new work is compared against:
// origin/HEAD's target when the remote sets one, else a local default
// branch, else "". Cheap and offline — no fetch.
func defaultRef(repoRoot string) string {
	if out, err := git(context.Background(), readTimeout, repoRoot,
		"symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(string(out))
	}
	for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := git(context.Background(), readTimeout, repoRoot,
			"rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// comparisonBase picks the ref a branch's unmerged work is measured
// against: its own upstream when it has one, else the repo default.
func comparisonBase(upstream, defaultBase string) string {
	if upstream != "" {
		return upstream
	}
	return defaultBase
}

// mergedBranches returns the set of local branches fully reachable
// from base. Empty (nothing merged) when base is unresolvable, which
// keeps every branch looking unmerged — the conservative direction.
func mergedBranches(repoRoot, base string) map[string]bool {
	set := map[string]bool{}
	if base == "" {
		return set
	}
	out, err := git(context.Background(), readTimeout, repoRoot,
		"for-each-ref", "--format=%(refname:short)", "--merged", base, "refs/heads")
	if err != nil {
		return set
	}
	for _, name := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if name != "" {
			set[name] = true
		}
	}
	return set
}

// aheadCount returns how many commits ref has that base does not, and
// whether the question could be answered at all.
//
// ok=false is NOT the same as 0: an ambiguous ref, a pruned
// remote-tracking base or a damaged pack all make the count
// unavailable, and a caller that reads that as "nothing unpushed" will
// happily delete the commits. Inspect turns ok=false into
// Status.Unknown, which is not pristine.
func aheadCount(repoRoot, ref, base string) (int, bool) {
	if base == "" {
		return 0, false
	}
	// `--` separates revisions from paths: without it a ref that also
	// names a file on disk is ambiguous and git refuses.
	out, err := git(context.Background(), readTimeout, repoRoot,
		"rev-list", "--count", base+".."+ref, "--")
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return n, true
}

// Status is the safety verdict for one worktree: everything a caller
// needs to decide whether deleting it can lose work.
type Status struct {
	Branch      string
	Uncommitted bool
	Unpushed    int
	// Unknown is true when no comparison base resolved (no upstream,
	// no default ref) so Unpushed could not be computed. Callers MUST
	// treat it as unsafe to delete — guessing "clean" here is how work
	// disappears.
	Unknown bool
}

// Pristine reports whether the worktree can be removed without losing
// anything: no uncommitted changes, no unpushed commits, and the
// unpushed question actually answerable.
func (s Status) Pristine() bool {
	return !s.Uncommitted && s.Unpushed == 0 && !s.Unknown
}

// Inspect reports the safety status of the worktree at worktreePath.
// A missing directory is pristine — there is nothing left to lose.
func Inspect(repoRoot, worktreePath string) (Status, error) {
	var s Status
	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			// Genuinely gone: nothing left to lose, so it is disposable.
			return s, nil
		}
		// Anything else (EACCES after a parent's mode changed, EIO on a
		// network filesystem) means we could not look — which is not the
		// same as "there is nothing there". Fail closed.
		s.Unknown = true
		return s, fmt.Errorf("stat worktree: %w", err)
	}
	dirty, err := HasUncommitted(worktreePath)
	if err != nil {
		return s, err
	}
	s.Uncommitted = dirty

	// Branch of THIS worktree, not the repo's current branch — hence
	// -C worktreePath. Detached HEAD yields "HEAD"; treat that as no
	// branch, and as unknown (we can't name a base to compare against).
	out, err := git(context.Background(), readTimeout, worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return s, err
	}
	s.Branch = strings.TrimSpace(string(out))
	if s.Branch == "HEAD" || s.Branch == "" {
		s.Branch = ""
		s.Unknown = true
		return s, nil
	}

	upstream := ""
	if o, uerr := git(context.Background(), readTimeout, worktreePath,
		"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); uerr == nil {
		upstream = strings.TrimSpace(string(o))
	}
	base := comparisonBase(upstream, defaultRef(repoRoot))
	if base == "" {
		s.Unknown = true
		return s, nil
	}
	n, ok := aheadCount(repoRoot, s.Branch, base)
	if !ok {
		// Could not compare: treat as holding work, never as clean.
		s.Unknown = true
		return s, nil
	}
	s.Unpushed = n
	return s, nil
}

// MoveWorktree relocates a worktree directory via `git worktree move`,
// which updates git's admin state as well as the directory. The
// destination must not exist.
func MoveWorktree(repoRoot, from, to string) error {
	if from == "" || to == "" {
		return errors.New("worktree.MoveWorktree: empty path")
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("worktree.MoveWorktree: destination %s already exists", to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}
	_, err := git(context.Background(), mutateTimeout, repoRoot, "worktree", "move", from, to)
	return err
}

// RenameBranch renames a local branch (`git branch -m`). Fails when
// the destination name is taken — we never pass -M, because forcing
// would silently discard the branch being overwritten.
func RenameBranch(repoRoot, oldName, newName string) error {
	if oldName == "" || newName == "" {
		return errors.New("worktree.RenameBranch: empty branch name")
	}
	if oldName == newName {
		return nil
	}
	_, err := git(context.Background(), mutateTimeout, repoRoot, "branch", "-m", oldName, newName)
	return err
}

// DeleteBranch removes a local branch. Without force this is `-d`,
// which git itself refuses for a branch holding unmerged commits.
func DeleteBranch(repoRoot, branch string, force bool) error {
	if branch == "" {
		return errors.New("worktree.DeleteBranch: empty branch name")
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := git(context.Background(), mutateTimeout, repoRoot, "branch", flag, branch)
	return err
}
