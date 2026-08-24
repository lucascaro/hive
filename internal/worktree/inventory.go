package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Read-path timeout. Every function here shells out to git for
// inspection only, so a hung filesystem must not stall the caller for
// long. Mutations reuse the package's 30s bound (see mutateTimeout).
const (
	readTimeout   = 5 * time.Second
	mutateTimeout = 30 * time.Second
	// How many per-branch git probes run at once. Bounded so a repo
	// with hundreds of branches does not fork hundreds of processes.
	probeConcurrency = 8
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
	Merged      bool   // reachable from the default ref
	Subject     string // first line of the branch tip's commit message
}

// ListBranches returns every local branch with worktree and
// merged-ness annotations. Never fetches: this is a read path that
// must not block on the network.
func ListBranches(repoRoot string) ([]BranchInfo, error) {
	base := defaultRef(repoRoot)
	list, err := forEachBranch(repoRoot, base)
	if err != nil {
		return nil, err
	}
	merged := mergedBranches(repoRoot, base)
	for i := range list {
		list[i].Merged = merged[list[i].Name]
	}

	// A squash merge rewrites the commits, so the branch is not an
	// ancestor of base and the set above misses it. Asking per branch
	// costs a walk of base's history each time; instead the history is
	// indexed by patch id once and every branch is matched against it.
	index := mergedPatchIDs(repoRoot, base)

	// Everything left is one git spawn per branch, so it runs on the
	// worker pool: a repo with a few hundred branches otherwise spends
	// seconds waiting on processes one at a time.
	forEachConcurrently(len(list), func(i int) {
		b := &list[i]
		// The batched ahead-behind above is measured against base. A
		// branch tracking something else needs its own comparison.
		//
		// Display only: an unanswerable count shows as 0 ahead here. The
		// deletion decision does not come from this list — it comes from
		// Inspect, which treats the same failure as Unknown.
		if cb := comparisonBase(b.Upstream, base); cb != base {
			b.Ahead, _ = aheadCount(repoRoot, b.Name, cb)
		}
		if len(index) > 0 && !b.Merged && b.Ahead > 0 {
			b.Merged = index[squashPatchID(repoRoot, b.Name, base)]
		}
	})
	return list, nil
}

// forEachBranch reads every local branch in one `git for-each-ref`,
// including its ahead count against base.
//
// %(ahead-behind:) needs git 2.41; on anything older for-each-ref
// fails outright, so the whole listing falls back to a format without
// it and counts per branch (the old behaviour, one spawn each).
func forEachBranch(repoRoot, base string) ([]BranchInfo, error) {
	// %(worktreepath) is empty for branches with no worktree; the
	// separator is \x00 so branch names containing spaces survive.
	// %(subject) is the tip's commit subject — one line, and free
	// here, where asking per branch would be a spawn each.
	const format = "%(refname:short)%00%(upstream:short)%00%(worktreepath)%00%(subject)"
	batched := base != ""
	f := format
	if batched {
		f += "%00%(ahead-behind:" + base + ")"
	}
	out, err := git(context.Background(), readTimeout, repoRoot,
		"for-each-ref", "--format="+f, "refs/heads")
	if err != nil {
		if !batched {
			return nil, err
		}
		batched = false
		out, err = git(context.Background(), readTimeout, repoRoot,
			"for-each-ref", "--format="+format, "refs/heads")
		if err != nil {
			return nil, err
		}
	}

	var list []BranchInfo
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 4 || parts[0] == "" {
			continue
		}
		b := BranchInfo{
			Name:        parts[0],
			Upstream:    parts[1],
			HasWorktree: parts[2] != "",
			Subject:     parts[3],
		}
		if batched && len(parts) > 4 {
			// "<ahead> <behind>"; an unresolvable base prints nothing.
			if ahead, _, ok := strings.Cut(parts[4], " "); ok {
				b.Ahead, _ = strconv.Atoi(ahead)
			}
		} else {
			b.Ahead, _ = aheadCount(repoRoot, b.Name, base)
		}
		list = append(list, b)
	}
	return list, nil
}

// forEachConcurrently runs fn for indices [0,n) over a bounded worker
// pool. The work here is all `git` subprocesses — spawn latency, not
// CPU — so a small fixed fan-out is what turns seconds into
// milliseconds.
func forEachConcurrently(n int, fn func(i int)) {
	if n <= 0 {
		return
	}
	workers := probeConcurrency
	if n < workers {
		workers = n
	}
	var wg sync.WaitGroup
	idx := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		idx <- i
	}
	close(idx)
	wg.Wait()
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

// IsAncestor reports whether local branch ref is reachable from base —
// the single-branch form of mergedBranches, without listing every ref.
// base may be any committish, including a raw sha; an unknown one
// reports false rather than erroring, which is the safe direction for
// every caller here.
func IsAncestor(repoRoot, ref, base string) bool {
	if ref == "" || base == "" {
		return false
	}
	_, err := git(context.Background(), readTimeout, repoRoot,
		"merge-base", "--is-ancestor", "refs/heads/"+ref, base)
	return err == nil
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

// A squash merge collapses a branch into a single commit with a new
// sha, so reachability cannot see it. What survives is the patch: the
// squash commit's diff equals the branch's cumulative diff, and git
// can name that equality with `git patch-id`.
//
// This is a heuristic. A squash that was edited on the way in
// (conflict resolution, a rebase onto newer main) has a different
// patch id and reads as unmerged — the safe direction, since callers
// treat unmerged as "may still hold work".

// patchIDHistory bounds how far back the index reads. A squash older
// than this reads as unmerged; the alternative is walking the diff of
// an entire repository's history on a read path.
const patchIDHistory = "2000"

// mergedPatchIDs indexes base's history by patch id — one walk that
// answers the question for every branch, instead of one walk each.
func mergedPatchIDs(repoRoot, base string) map[string]bool {
	if base == "" {
		return nil
	}
	ids, err := gitPatchIDs(repoRoot,
		"log", "--format=%H", "-p", "--no-color", "--max-count="+patchIDHistory, base)
	if err != nil {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// squashPatchID returns the patch id of everything ref adds on top of
// base — the patch a squash merge of ref would have produced. Empty
// when it cannot be computed, which never matches an index entry.
//
// `base...ref` (three dots) diffs from the merge base, so git finds
// the branch point itself and no separate merge-base call is needed.
func squashPatchID(repoRoot, ref, base string) string {
	if ref == "" || base == "" {
		return ""
	}
	// Fully qualified so a branch sharing its name with a file on disk
	// is not ambiguous.
	ids, err := gitPatchIDs(repoRoot, "diff", base+"...refs/heads/"+ref)
	if err != nil || len(ids) != 1 {
		return ""
	}
	return ids[0]
}

// gitPatchIDs runs `git <args…> | git patch-id --stable` and returns
// the patch ids. Streamed rather than buffered: the left-hand side can
// be an entire history's worth of diff.
func gitPatchIDs(repoRoot string, args ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	producer := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	producer.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	pipe, err := producer.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// --stable so the id does not depend on the order git happened to
	// emit the hunks in.
	consumer := exec.CommandContext(ctx, "git", "-C", repoRoot, "patch-id", "--stable")
	consumer.Env = producer.Env
	consumer.Stdin = pipe
	if err := producer.Start(); err != nil {
		return nil, err
	}
	out, cerr := consumer.Output()
	// Drain and reap the producer either way — a patch-id that exits
	// first leaves it writing into a closed pipe, and an unwaited
	// process is a zombie.
	pipe.Close()
	_ = producer.Wait()
	if cerr != nil {
		return nil, cerr
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if id, _, ok := strings.Cut(line, " "); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// squashMerged answers the same question as the index above for a
// single branch, without paying to index all of base — the shape
// Inspect needs.
func squashMerged(repoRoot, ref, base string) bool {
	id := squashPatchID(repoRoot, ref, base)
	if id == "" {
		return false
	}
	// Only base's commits since the branch point can contain the
	// squash, so the walk stops there — and is capped the same way the
	// index is, so both paths answer from the same window rather than
	// disagreeing about the same branch.
	ids, err := gitPatchIDs(repoRoot,
		"log", "--format=%H", "-p", "--no-color", "--max-count="+patchIDHistory,
		base, "--not", "refs/heads/"+ref)
	if err != nil {
		return false
	}
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
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
	// Upstream is the branch's tracking ref ("origin/foo"), "" when it
	// tracks nothing — which is also "there is no remote branch to
	// delete".
	Upstream string
	// Merged is true when the branch's work is already in the repo's
	// default ref, whether by a plain merge or a squash. It is a
	// heuristic (see squashMerged) that only ever errs towards false,
	// so Unpushed commits on a merged branch are not lost work.
	Merged bool
}

// Pristine reports whether the worktree can be removed without losing
// anything: no uncommitted changes, no unpushed commits, and the
// unpushed question actually answerable.
//
// Deliberately blind to Merged. The unconfirmed auto-delete paths —
// session Kill, boot-time orphan reclaim — gate on this, and a
// heuristic must not be what widens them. Merged only relaxes the
// browser's own delete, which is an explicit, confirmed act.
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
	s.Upstream = upstream
	// Merged is always measured against the default ref, never the
	// upstream: a squash-merged branch's own origin/<branch> still
	// exists and would answer the wrong question.
	dflt := defaultRef(repoRoot)
	s.Merged = IsAncestor(repoRoot, s.Branch, dflt) || squashMerged(repoRoot, s.Branch, dflt)

	base := comparisonBase(upstream, dflt)
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

// UpstreamOf returns the remote name and the branch name on that
// remote for a local branch, or empty strings when it tracks nothing.
// The two are read from git rather than split out of "origin/foo":
// remote names and branch names may both contain slashes, so that
// split is guesswork.
func UpstreamOf(repoRoot, branch string) (remote, remoteBranch string) {
	if branch == "" {
		return "", ""
	}
	out, err := git(context.Background(), readTimeout, repoRoot, "for-each-ref",
		"--format=%(upstream:remotename)%00%(upstream:remoteref)",
		"refs/heads/"+branch)
	if err != nil {
		return "", ""
	}
	name, ref, _ := strings.Cut(strings.TrimSpace(string(out)), "\x00")
	if name == "" || ref == "" {
		return "", ""
	}
	return name, strings.TrimPrefix(ref, "refs/heads/")
}

// DeleteRemoteBranch deletes a branch on a remote. This is a network
// operation and the one thing here the user cannot undo locally, so it
// is never inferred — a caller asks for it explicitly.
//
// A branch already gone from the remote is not an error: someone else
// (or GitHub's own "delete branch on merge") got there first, and the
// end state is the one that was asked for.
func DeleteRemoteBranch(repoRoot, remote, branch string) error {
	if remote == "" || branch == "" {
		return errors.New("worktree.DeleteRemoteBranch: empty remote or branch")
	}
	// Fully qualified: a branch named like a flag ("-x") would
	// otherwise be parsed as one.
	out, err := git(context.Background(), mutateTimeout, repoRoot,
		"push", remote, "--delete", "refs/heads/"+branch)
	if err != nil && strings.Contains(string(out), "remote ref does not exist") {
		return nil
	}
	if err != nil {
		// git echoes the remote URL back, and an HTTPS remote can carry
		// a token in its userinfo. The error travels to the GUI and
		// into logs, so strip that before it leaves this function.
		return errors.New(scrubURLCredentials(err.Error()))
	}
	return nil
}

// credentialsInURL matches the userinfo half of a URL — "//user:token@".
var credentialsInURL = regexp.MustCompile(`//[^/@\s]+@`)

func scrubURLCredentials(s string) string {
	return credentialsInURL.ReplaceAllString(s, "//***@")
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
