// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/proc"
	"github.com/lucascaro/hive/internal/wire"
)

// Workflow worktrees (spec 528): the git half of PREPARE_WORKTREE and
// COMMIT_WORKTREE. A workflow run owns the branches under
// hive/wf/<run>/; this file creates worktrees on them, merges run
// branches together and commits their work.
//
// Every git call here goes through safeGit, and every argument that
// came from a plugin is validated before it reaches git AND placed
// where git cannot read it as an option (after `--` or
// `--end-of-options`).

// Sentinels the registry and daemon map onto wire error codes.
var (
	// ErrInvalidRunID: the workflow run id is not a valid run id.
	ErrInvalidRunID = errors.New("worktree: invalid workflow run id")
	// ErrInvalidWorkflowBranch: not a valid hive/wf/<run>/... ref name.
	ErrInvalidWorkflowBranch = errors.New("worktree: invalid workflow branch")
	// ErrNoCommit: a revision did not resolve to a commit.
	ErrNoCommit = errors.New("worktree: revision does not resolve to a commit")
	// ErrIdentityMissing: the repo has no configured git identity.
	ErrIdentityMissing = errors.New("worktree: git identity (user.name, user.email) is not configured")
	// ErrNotManaged: the worktree is outside <root>/.worktrees.
	ErrNotManaged = errors.New("worktree: not a hive-managed worktree")
	// ErrDirty: a merge was refused because the worktree holds
	// uncommitted work that a failed merge could not give back.
	ErrDirty = errors.New("worktree: worktree has uncommitted changes")
)

// RunBranchPrefix is the namespace a workflow run owns: every branch it
// creates, merges or commits lives under it.
func RunBranchPrefix(runID string) string { return "hive/wf/" + runID + "/" }

// workflowBranchRe is deliberately narrower than git's ref grammar:
// plain ASCII, so a branch needs no quoting anywhere it travels (argv,
// a directory name, a commit message) and cannot smuggle an option, a
// path separator trick or a control character.
var workflowBranchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

const maxWorkflowBranch = 200

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// CheckBranchName reports whether branch is a legal branch for the run:
// under RunBranchPrefix(runID), within the ASCII grammar above, no
// leading `-`, and accepted by `git check-ref-format --branch`.
func CheckBranchName(ctx context.Context, repoDir, runID, branch string) error {
	if !wire.ValidWorkflowRunID(runID) {
		return fmt.Errorf("%w: %q", ErrInvalidRunID, runID)
	}
	prefix := RunBranchPrefix(runID)
	switch {
	case strings.HasPrefix(branch, "-"):
		return fmt.Errorf("%w: %q starts with '-'", ErrInvalidWorkflowBranch, branch)
	case len(branch) > maxWorkflowBranch:
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidWorkflowBranch, maxWorkflowBranch)
	case !strings.HasPrefix(branch, prefix) || len(branch) == len(prefix):
		return fmt.Errorf("%w: %q is not under %s", ErrInvalidWorkflowBranch, branch, prefix)
	case !workflowBranchRe.MatchString(branch):
		return fmt.Errorf("%w: %q has characters outside [A-Za-z0-9._/-]", ErrInvalidWorkflowBranch, branch)
	}
	out, err := safeGit(ctx, repoDir, "check-ref-format", "--branch", branch)
	if err != nil || strings.TrimSpace(string(out)) != branch {
		return fmt.Errorf("%w: %q is not a valid ref name", ErrInvalidWorkflowBranch, branch)
	}
	return nil
}

// ResolveCommit resolves rev to the SHA of the commit it names. The SHA
// is what callers hand to git from then on, never the name: a ref that
// moves after this returns cannot change what gets checked out or merged.
func ResolveCommit(ctx context.Context, repoDir, rev string) (string, error) {
	switch {
	case rev == "" || len(rev) > 256:
		return "", fmt.Errorf("%w: empty or too long", ErrNoCommit)
	case strings.HasPrefix(rev, ":/") || strings.Contains(rev, "^{/"):
		return "", fmt.Errorf("%w: %q would search all of history", ErrNoCommit, rev)
	case strings.HasPrefix(rev, "-"):
		return "", fmt.Errorf("%w: %q looks like an option", ErrNoCommit, rev)
	case strings.ContainsFunc(rev, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return "", fmt.Errorf("%w: control character", ErrNoCommit)
	}
	out, err := safeGit(ctx, repoDir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	sha := strings.TrimSpace(string(out))
	if err != nil || !shaRe.MatchString(sha) {
		return "", fmt.Errorf("%w: %q", ErrNoCommit, rev)
	}
	return sha, nil
}

// localBranchExists reports whether refs/heads/<branch> exists, via safeGit.
func localBranchExists(ctx context.Context, repoDir, branch string) bool {
	_, err := safeGit(ctx, repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// FindBranchWorktree returns the registered worktree that has branch
// checked out, if any. A prunable entry (its directory is gone) is
// reported as such so the caller can prune it before re-adding.
func FindBranchWorktree(ctx context.Context, repoDir, branch string) (info Info, found bool, err error) {
	// -z: a path may hold a newline, which must not read as more fields.
	out, err := safeGit(ctx, repoDir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Info{}, false, err
	}
	for _, wt := range parseWorktreeListZ(string(out)) {
		if wt.Branch == branch {
			return wt, true, nil
		}
	}
	return Info{}, false, nil
}

// PruneWorktrees drops git's admin entries for worktrees whose
// directories are gone.
func PruneWorktrees(ctx context.Context, repoDir string) error {
	_, err := safeGit(ctx, repoDir, "worktree", "prune")
	return err
}

// AddNewBranch creates branch at sha in a new worktree at path. It is
// strict: there is no fallback to an existing branch, so a branch that
// already exists is an error, never silently checked out.
func AddNewBranch(ctx context.Context, repoDir, branch, path, sha string) error {
	if !shaRe.MatchString(sha) {
		return fmt.Errorf("worktree: %q is not a commit SHA", sha)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}
	_, err := safeGit(ctx, repoDir, "worktree", "add", "-b", branch, "--", path, sha)
	return err
}

// AddExistingBranch checks the existing local branch out into a new
// worktree at path, and verifies HEAD really is that branch (a tag of
// the same name must not turn this into a detached checkout).
func AddExistingBranch(ctx context.Context, repoDir, branch, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}
	if _, err := safeGit(ctx, repoDir, "worktree", "add", "--", path, branch); err != nil {
		return err
	}
	out, err := safeGit(ctx, path, "symbolic-ref", "-q", "HEAD")
	if err != nil || strings.TrimSpace(string(out)) != "refs/heads/"+branch {
		_ = Cleanup(repoDir, path)
		return fmt.Errorf("worktree: %s did not check out branch %s", path, branch)
	}
	return nil
}

// FreePath picks the directory for a new worktree on branch:
// <root>/.worktrees/<sanitized branch>, suffixed -2, -3, … while taken.
// Only the directory is suffixed, never the branch: the branch name is
// the caller's contract. It refuses a .worktrees that is a symlink (a
// repo can commit one, and every worktree would then land wherever it
// points).
func FreePath(root, branch string) (string, error) {
	dir := ManagedDir(root)
	if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s is a symlink", ErrNotManaged, dir)
	}
	base := WorktreePath(root, branch)
	candidate := base
	for suffix := 2; suffix < 100; suffix++ {
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			if !IsManaged(root, candidate) {
				return "", fmt.Errorf("%w: %s", ErrNotManaged, candidate)
			}
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
	return "", fmt.Errorf("worktree: too many collisions for %q", base)
}

// Prepared is the outcome of PrepareRun.
type Prepared struct {
	Path       string
	Reattached bool
	// BaseSHA is the commit base resolved to; empty when Reattached.
	BaseSHA string
	HeadSHA string
	Merged  []string
	// Conflict, when set, left the worktree at base plus Merged.
	Conflict *MergeConflict
}

// PrepareRun makes a worktree on branch for the run, optionally merging
// the branches in merge into it. root is the project's main checkout;
// the caller serializes git work on the repo.
//
// A branch that already exists is re-attached (its worktree's path, or a
// fresh worktree on it) and base is ignored. A new one is created from
// the SHA base resolves to. Every name is validated before any git
// mutation, so a bad request leaves nothing behind.
func PrepareRun(ctx context.Context, root, runID, branch, base string, merge []string) (Prepared, error) {
	if err := requireSupportedGit(ctx); err != nil {
		return Prepared{}, err
	}
	if err := CheckBranchName(ctx, root, runID, branch); err != nil {
		return Prepared{}, err
	}
	if len(merge) > maxMergeBranches {
		return Prepared{}, fmt.Errorf("%w: %d merge branches; the limit is %d", ErrInvalidWorkflowBranch, len(merge), maxMergeBranches)
	}
	shas := make([]NamedSHA, 0, len(merge))
	seen := map[string]bool{}
	for _, m := range merge {
		if seen[m] {
			continue
		}
		seen[m] = true
		if err := ctx.Err(); err != nil {
			return Prepared{}, err
		}
		if err := CheckBranchName(ctx, root, runID, m); err != nil {
			return Prepared{}, fmt.Errorf("merge: %w", err)
		}
		if m == branch {
			return Prepared{}, fmt.Errorf("%w: cannot merge %s into itself", ErrInvalidWorkflowBranch, m)
		}
		// refs/heads/ explicitly: a tag or other ref of the same short
		// name must not stand in for the branch.
		sha, err := ResolveCommit(ctx, root, "refs/heads/"+m)
		if err != nil {
			return Prepared{}, fmt.Errorf("merge: %w: branch %s does not exist", ErrInvalidWorkflowBranch, m)
		}
		shas = append(shas, NamedSHA{Name: m, SHA: sha})
	}

	var out Prepared
	existing, found, err := FindBranchWorktree(ctx, root, branch)
	if err != nil {
		return Prepared{}, err
	}
	switch {
	case found && !existing.Prunable:
		if !IsManaged(root, existing.Path) {
			return Prepared{}, fmt.Errorf("%w: %s has %s checked out", ErrNotManaged, existing.Path, branch)
		}
		out.Path, out.Reattached = existing.Path, true
	case found || localBranchExists(ctx, root, branch):
		if found { // its directory is gone; drop the stale entry first
			if err := PruneWorktrees(ctx, root); err != nil {
				return Prepared{}, err
			}
		}
		if out.Path, err = FreePath(root, branch); err != nil {
			return Prepared{}, err
		}
		if err = AddExistingBranch(ctx, root, branch, out.Path); err != nil {
			return Prepared{}, err
		}
		out.Reattached = true
	default:
		if out.BaseSHA, err = ResolveCommit(ctx, root, base); err != nil {
			return Prepared{}, fmt.Errorf("base: %w", err)
		}
		if out.Path, err = FreePath(root, branch); err != nil {
			return Prepared{}, err
		}
		if err = AddNewBranch(ctx, root, branch, out.Path, out.BaseSHA); err != nil {
			return Prepared{}, err
		}
	}

	if err := verifyWorktreeLink(ctx, root, out.Path); err != nil {
		return Prepared{}, err
	}
	if out.Merged, out.Conflict, err = MergeBranches(ctx, out.Path, shas); err != nil {
		return Prepared{}, err
	}
	if out.HeadSHA, err = Head(ctx, out.Path); err != nil {
		return Prepared{}, err
	}
	return out, nil
}

// CommitRun commits everything in the worktree that has branch checked
// out (see CommitAll). branch must be under the run's namespace.
func CommitRun(ctx context.Context, root, runID, branch, message string) (committed bool, head string, err error) {
	if err := requireSupportedGit(ctx); err != nil {
		return false, "", err
	}
	if err := CheckBranchName(ctx, root, runID, branch); err != nil {
		return false, "", err
	}
	if message == "" || len(message) > maxCommitMessage || strings.ContainsRune(message, 0) {
		return false, "", fmt.Errorf("worktree: commit message must be 1..%d bytes with no NUL", maxCommitMessage)
	}
	wt, found, err := FindBranchWorktree(ctx, root, branch)
	if err != nil {
		return false, "", err
	}
	if !found || wt.Prunable || !IsManaged(root, wt.Path) {
		return false, "", fmt.Errorf("%w: no managed worktree has %s checked out", ErrNotManaged, branch)
	}
	if err := verifyWorktreeLink(ctx, root, wt.Path); err != nil {
		return false, "", err
	}
	return CommitAll(ctx, wt.Path, message)
}

// verifyWorktreeLink checks that path/.git is the gitfile git itself
// wrote for a worktree of root's repository: a regular file naming an
// admin dir under root's common dir, which in turn names path/.git
// back. The gitfile sits inside the working directory, where an agent
// can rewrite it; pointing it at a gitdir of its own would make every
// later git call there read that dir's config (filters, hooks) instead
// of the repository's.
func verifyWorktreeLink(ctx context.Context, root, path string) error {
	bad := func(why string) error {
		return fmt.Errorf("%w: %s: %s", ErrNotManaged, path, why)
	}
	gitfile := filepath.Join(path, ".git")
	fi, err := os.Lstat(gitfile)
	if err != nil || !fi.Mode().IsRegular() {
		return bad(".git is not a regular file")
	}
	body, err := os.ReadFile(gitfile)
	if err != nil {
		return bad("cannot read .git")
	}
	line, _, _ := strings.Cut(string(body), "\n")
	admin, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "gitdir: ")
	if !ok || admin == "" {
		return bad(".git is not a gitdir file")
	}
	if !filepath.IsAbs(admin) {
		admin = filepath.Join(path, admin)
	}
	out, err := safeGit(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	common := ResolvePath(strings.TrimSpace(string(out)))
	admin = ResolvePath(admin)
	if filepath.Dir(admin) != filepath.Join(common, "worktrees") {
		return bad(".git points outside the repository's worktree admin dir")
	}
	back, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil || ResolvePath(strings.TrimSpace(string(back))) != ResolvePath(gitfile) {
		return bad("the admin dir does not point back at this .git")
	}
	return nil
}

const (
	maxCommitMessage = 16 << 10
	// maxMergeBranches bounds the git calls one PREPARE_WORKTREE makes
	// while holding the registry's git lock.
	maxMergeBranches = 64
)

// Head returns the SHA HEAD points at in dir.
func Head(ctx context.Context, dir string) (string, error) {
	out, err := safeGit(ctx, dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// NamedSHA is a branch and the commit it resolved to.
type NamedSHA struct {
	Name string
	SHA  string
}

// MergeConflict is a merge that stopped on conflicts.
type MergeConflict struct {
	Branch string
	Files  []string
}

// MergeBranches merges each of shas into the worktree at wt, in order,
// as no-fast-forward merge commits by the user's identity. It stops at
// the first conflict: the merge is aborted so the tree is clean, and the
// branches merged so far stay merged. A merge of something already in
// the branch is a no-op, so repeating the same list is safe.
func MergeBranches(ctx context.Context, wt string, shas []NamedSHA) (merged []string, conflict *MergeConflict, err error) {
	if len(shas) == 0 {
		return nil, nil, nil
	}
	if err := checkIdentity(ctx, wt); err != nil {
		return nil, nil, err
	}
	// A merge that conflicts is aborted, and git cannot always restore
	// local changes it had to stash for it: refuse rather than risk work
	// (a re-attached worktree may hold the user's or an agent's edits).
	if dirty, err := worktreeDirty(ctx, wt); err != nil {
		return nil, nil, err
	} else if dirty {
		return nil, nil, fmt.Errorf("%w: commit or discard it before merging into %s", ErrDirty, wt)
	}
	for _, nb := range shas {
		if !shaRe.MatchString(nb.SHA) {
			return merged, nil, fmt.Errorf("worktree: %q is not a commit SHA", nb.SHA)
		}
		_, mergeErr := safeGit(ctx, wt, "merge", "--no-ff", "--commit", "--no-edit", "--no-verify", "--no-gpg-sign",
			"-m", "Merge branch '"+nb.Name+"'", nb.SHA)
		if mergeErr == nil {
			merged = append(merged, nb.Name)
			continue
		}
		out, listErr := safeGit(ctx, wt, "diff", "--no-ext-diff", "--name-only", "--diff-filter=U", "-z")
		var files []string
		if listErr == nil {
			for _, f := range strings.Split(string(out), "\x00") {
				if f != "" {
					files = append(files, f)
				}
			}
		}
		// Abort whatever state the failed merge left (harmless when none).
		// Cleanup runs on its own bounded context: a canceled request must
		// not leave the tree mid-merge.
		cleanCtx, cancelClean := context.WithTimeout(context.WithoutCancel(ctx), mutateTimeout)
		_, abortErr := safeGit(cleanCtx, wt, "merge", "--abort")
		cancelClean()
		if len(files) == 0 {
			return merged, nil, fmt.Errorf("merge %s: %w", nb.Name, mergeErr)
		}
		if abortErr != nil {
			return merged, nil, fmt.Errorf("merge %s conflicted in %d file(s) and could not be aborted: %w", nb.Name, len(files), abortErr)
		}
		return merged, &MergeConflict{Branch: nb.Name, Files: files}, nil
	}
	return merged, nil, nil
}

// CommitAll stages everything in the worktree at wt (except the agent
// config Hive itself linked in) and commits it with message, by the
// user's identity and without running hooks. A clean tree is a no-op
// (committed false), so the call is idempotent. head is HEAD afterwards.
func CommitAll(ctx context.Context, wt, message string) (committed bool, head string, err error) {
	add := append([]string{"add", "-A", "--", "."}, agentConfigExcludes(wt)...)
	if _, err := safeGit(ctx, wt, add...); err != nil {
		return false, "", err
	}
	out, err := safeGit(ctx, wt, "diff", "--no-ext-diff", "--cached", "--name-only", "-z")
	if err != nil {
		return false, "", err
	}
	if len(out) > 0 {
		// Identity matters only when there is something to commit: a clean
		// tree stays a no-op.
		if err := checkIdentity(ctx, wt); err != nil {
			return false, "", err
		}
		if _, err := safeGit(ctx, wt, "commit", "--no-verify", "--no-gpg-sign", "-m", message); err != nil {
			return false, "", err
		}
		committed = true
	}
	head, err = Head(ctx, wt)
	return committed, head, err
}

// worktreeDirty reports uncommitted work in wt: tracked changes or
// untracked files, ignoring the agent config Hive itself linked in. It
// is HasUncommitted through safeGit.
func worktreeDirty(ctx context.Context, wt string) (bool, error) {
	args := append([]string{"status", "--porcelain", "-uall", "-z", "--", "."}, agentConfigExcludes(wt)...)
	out, err := safeGit(ctx, wt, args...)
	return len(out) > 0, err
}

// checkIdentity fails with ErrIdentityMissing when git cannot name an
// author and committer from configuration alone. There is no fallback
// identity: a commit by someone else would be a lie in the history.
func checkIdentity(ctx context.Context, dir string) error {
	for _, v := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		if _, err := safeGit(ctx, dir, "var", v); err != nil {
			return fmt.Errorf("%w: %v", ErrIdentityMissing, err)
		}
	}
	return nil
}

// scrubbedEnv names the variables that would redirect git at another
// repository or index, whatever the repo itself says.
var scrubbedEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE", "GIT_PREFIX",
}

// safeGit runs `git -C dir <args…>` for the workflow path. Compared to
// git(): it turns off everything that runs code on an ordinary
// operation (hooks, fsmonitor, signing), refuses to guess an identity,
// ignores env that redirects git elsewhere, and returns stdout alone
// (stderr only in the error) so output that is parsed is never mixed
// with a warning. Merge drivers and filters from the repo's config and
// attributes still apply: they are the user's own repo.
func safeGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	hooks, err := noHooksPath()
	if err != nil {
		return nil, fmt.Errorf("worktree: no-hooks path: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, mutateTimeout)
	defer cancel()
	full := append([]string{"-C", dir}, safeGitConfig()...)
	full = append(full, []string{
		"-c", "core.hooksPath=" + hooks,
		"-c", "core.fsmonitor=false",
		// No repo-wide maintenance under the git lock: auto-gc would also
		// run pre-auto-gc hooks.
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
		"-c", "commit.gpgSign=false",
		"-c", "user.useConfigOnly=true",
	}...)
	full = append(full, args...)
	cmd := proc.CommandContext(ctx, "git", full...)
	env := make([]string, 0, len(os.Environ())+3)
envLoop:
	for _, kv := range os.Environ() {
		for _, bad := range scrubbedEnv {
			if strings.HasPrefix(kv, bad+"=") {
				continue envLoop
			}
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "LC_ALL=C", "LANG=C", "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A filter that outlives git and keeps its stderr open must not hold
	// the git lock: give up on the pipes shortly after the kill.
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return stdout.Bytes(), fmt.Errorf("git %s: timed out after %s", args[0], mutateTimeout)
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(scrubURLCredentials(stderr.String())), err)
	}
	return stdout.Bytes(), nil
}

// configHookEvents are the hook events the workflow git commands can
// fire (worktree add, commit, merge, merge --abort, branch, rev-parse,
// status). Config-defined hooks (git 2.54+) run beside the hooks
// directory, so each event is disabled by name; older git ignores the
// keys. TestSafeGit_ConfigHooksDoNotRun proves the list covers every
// event the real commands fire.
var configHookEvents = []string{
	"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit",
	"pre-merge-commit", "post-merge", "post-checkout", "reference-transaction",
	"post-index-change", "pre-auto-gc",
}

func safeGitConfig() []string {
	var out []string
	for _, ev := range configHookEvents {
		out = append(out, "-c", "hook."+ev+".enabled=false")
	}
	return out
}
