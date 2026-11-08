// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// workflowRepo is initRepo plus a repo-local identity, so merge and
// commit tests do not depend on the machine's global git config.
func workflowRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	mustGit(t, dir, "config", "user.name", "Test User")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	// The test machine's global config must not bring hooks or signing in.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return dir
}

const run = "r1"

func runBranch(name string) string { return RunBranchPrefix(run) + name }

// branchWith creates branch from base with one commit adding file=body.
func branchWith(t *testing.T, repo, branch, base, file, body string) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "wt")
	mustGit(t, repo, "worktree", "add", "-b", branch, tmp, base)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(tmp, file)), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmp, file), body)
	mustGit(t, tmp, "add", "-A")
	mustGit(t, tmp, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "add "+file)
	mustGit(t, repo, "worktree", "remove", "--force", tmp)
}

func TestCheckBranchName(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, run, branch string
		wantErr           error
	}{
		{"ok", run, runBranch("fix_2/work"), nil},
		{"ok nested", run, runBranch("work/each_3/do"), nil},
		{"wrong run", run, "hive/wf/other/x", ErrInvalidWorkflowBranch},
		{"outside namespace", run, "main", ErrInvalidWorkflowBranch},
		{"prefix only", run, RunBranchPrefix(run), ErrInvalidWorkflowBranch},
		{"leading dash", run, "-b", ErrInvalidWorkflowBranch},
		{"leading dash after prefix is fine but option-like whole is not", run, "--force", ErrInvalidWorkflowBranch},
		{"space", run, runBranch("a b"), ErrInvalidWorkflowBranch},
		{"dotdot", run, runBranch("a..b"), ErrInvalidWorkflowBranch},
		{"lock suffix", run, runBranch("a.lock"), ErrInvalidWorkflowBranch},
		{"trailing slash", run, runBranch("a/"), ErrInvalidWorkflowBranch},
		{"double slash", run, runBranch("a//b"), ErrInvalidWorkflowBranch},
		{"newline", run, runBranch("a\nb"), ErrInvalidWorkflowBranch},
		{"nul", run, runBranch("a\x00b"), ErrInvalidWorkflowBranch},
		{"non-ascii", run, runBranch("é"), ErrInvalidWorkflowBranch},
		{"too long", run, runBranch(strings.Repeat("a", 300)), ErrInvalidWorkflowBranch},
		{"bad run id", "Bad/Run", "hive/wf/Bad/Run/x", ErrInvalidRunID},
		{"empty run id", "", "hive/wf//x", ErrInvalidRunID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckBranchName(ctx, repo, tc.run, tc.branch)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("CheckBranchName(%q, %q) = %v, want %v", tc.run, tc.branch, err, tc.wantErr)
			}
		})
	}
}

func TestResolveCommit(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	head := revParse(t, repo, "HEAD")
	for _, rev := range []string{"HEAD", "main", head, head[:10], "refs/heads/main"} {
		got, err := ResolveCommit(ctx, repo, rev)
		if err != nil || got != head {
			t.Errorf("ResolveCommit(%q) = %q, %v; want %s", rev, got, err, head)
		}
	}
	marker := filepath.Join(t.TempDir(), "marker")
	for _, rev := range []string{"", "nope", "-h", "--output=" + marker, "--end-of-options", "HEAD\n--help", "main\x00x", "HEAD:nofile", ":/x", "HEAD^{/x}"} {
		if got, err := ResolveCommit(ctx, repo, rev); !errors.Is(err, ErrNoCommit) {
			t.Errorf("ResolveCommit(%q) = %q, %v; want ErrNoCommit", rev, got, err)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("an option-like base was interpreted by git (marker file written)")
	}
}

func TestAddNewBranch_NoExistingFallback(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	b := runBranch("a")
	mustGit(t, repo, "branch", b)
	path := filepath.Join(t.TempDir(), "wt")
	if err := AddNewBranch(ctx, repo, b, path, revParse(t, repo, "HEAD")); err == nil {
		t.Fatal("AddNewBranch on an existing branch succeeded; it must not fall back to checking it out")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a worktree was created for the refused branch")
	}
	if err := AddNewBranch(ctx, repo, runBranch("b"), path, "main"); err == nil {
		t.Error("AddNewBranch accepted a name where a SHA is required")
	}
}

func TestAddExistingBranch(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	b := runBranch("a")
	mustGit(t, repo, "branch", b)
	path := filepath.Join(t.TempDir(), "wt")
	if err := AddExistingBranch(ctx, repo, b, path); err != nil {
		t.Fatalf("AddExistingBranch: %v", err)
	}
	if got := strings.TrimSpace(gitOut(t, path, "symbolic-ref", "HEAD")); got != "refs/heads/"+b {
		t.Errorf("HEAD = %s, want refs/heads/%s", got, b)
	}
	// Already checked out elsewhere: git refuses, so do we.
	if err := AddExistingBranch(ctx, repo, b, filepath.Join(t.TempDir(), "wt2")); err == nil {
		t.Error("a branch checked out twice was accepted")
	}
}

func TestFreePath_SuffixesDirOnly(t *testing.T) {
	repo := workflowRepo(t)
	b := runBranch("fix_2/work")
	p1, err := FreePath(repo, b)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repo, ".worktrees", "hive-wf-r1-fix_2-work"); p1 != want {
		t.Errorf("path = %s, want %s", p1, want)
	}
	mustMkdir(t, p1)
	p2, err := FreePath(repo, b)
	if err != nil || p2 != p1+"-2" {
		t.Errorf("second path = %s, %v; want %s-2", p2, err, p1)
	}
}

func TestFreePath_RefusesSymlinkedManagedDir(t *testing.T) {
	repo := workflowRepo(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(repo, ".worktrees")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if p, err := FreePath(repo, runBranch("a")); !errors.Is(err, ErrNotManaged) {
		t.Errorf("FreePath through a symlinked .worktrees = %q, %v; want ErrNotManaged", p, err)
	}
}

func TestPrepareWorktree_PassesResolvedSHA(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	old := revParse(t, repo, "HEAD")
	sha, err := ResolveCommit(ctx, repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	// main moves after the resolve; the SHA, not the name, must win.
	mustGit(t, repo, "commit", "--allow-empty", "-q", "-m", "moved")
	path := filepath.Join(t.TempDir(), "wt")
	if err := AddNewBranch(ctx, repo, runBranch("a"), path, sha); err != nil {
		t.Fatal(err)
	}
	if got := revParse(t, path, "HEAD"); got != old {
		t.Errorf("worktree at %s, want the resolved %s", got, old)
	}
}

func TestPrepareRun_NewReattachAndPrune(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	base := revParse(t, repo, "HEAD")
	b := runBranch("a")

	p, err := PrepareRun(ctx, repo, run, b, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Reattached || p.BaseSHA != base || p.HeadSHA != base || len(p.Merged) != 0 {
		t.Errorf("new: %+v", p)
	}
	if !IsManaged(repo, p.Path) {
		t.Errorf("path %s is not managed", p.Path)
	}

	// Same branch while its worktree exists: that worktree, base ignored.
	p2, err := PrepareRun(ctx, repo, run, b, "no-such-base", nil)
	if err != nil || !p2.Reattached || p2.BaseSHA != "" || ResolvePath(p2.Path) != ResolvePath(p.Path) {
		t.Errorf("reattach: %+v, %v (first %s)", p2, err, p.Path)
	}

	// The worktree vanishes (boot reclaim): a fresh one on the SAME branch.
	if err := Cleanup(repo, p.Path); err != nil {
		t.Fatal(err)
	}
	p3, err := PrepareRun(ctx, repo, run, b, "main", nil)
	if err != nil || !p3.Reattached || p3.BaseSHA != "" {
		t.Fatalf("after reclaim: %+v, %v", p3, err)
	}
	if got := strings.TrimSpace(gitOut(t, p3.Path, "symbolic-ref", "HEAD")); got != "refs/heads/"+b {
		t.Errorf("HEAD = %s", got)
	}

	// Directory deleted without git knowing (prunable entry).
	if err := os.RemoveAll(p3.Path); err != nil {
		t.Fatal(err)
	}
	p4, err := PrepareRun(ctx, repo, run, b, "main", nil)
	if err != nil || !p4.Reattached {
		t.Fatalf("after rm -rf: %+v, %v", p4, err)
	}
}

func TestPrepareRun_Refusals(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	if _, err := PrepareRun(ctx, repo, run, "main", "main", nil); !errors.Is(err, ErrInvalidWorkflowBranch) {
		t.Errorf("branch outside the run prefix: %v", err)
	}
	if _, err := PrepareRun(ctx, repo, run, runBranch("a"), "--output=x", nil); !errors.Is(err, ErrNoCommit) {
		t.Errorf("option-like base: %v", err)
	}
	if _, err := PrepareRun(ctx, repo, run, runBranch("a"), "", nil); !errors.Is(err, ErrNoCommit) {
		t.Errorf("empty base: %v", err)
	}
	// A merge input outside the run's namespace, or one that is a tag.
	mustGit(t, repo, "tag", runBranch("tagged"))
	for _, m := range []string{"main", runBranch("missing"), runBranch("tagged"), runBranch("a")} {
		if _, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", []string{m}); !errors.Is(err, ErrInvalidWorkflowBranch) {
			t.Errorf("merge %q: %v, want ErrInvalidWorkflowBranch", m, err)
		}
	}
	// None of the refusals left a branch or a worktree behind.
	if BranchExists(repo, runBranch("a")) {
		t.Error("a refused prepare created the branch")
	}
	if _, err := os.Stat(filepath.Join(repo, ".worktrees")); err == nil {
		t.Error("a refused prepare created .worktrees")
	}
}

func TestMergeBranches_Clean(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	branchWith(t, repo, runBranch("n1"), "main", "a.txt", "a\n")
	branchWith(t, repo, runBranch("n2"), "main", "b.txt", "b\n")
	p, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main", []string{runBranch("n1"), runBranch("n2")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Conflict != nil || len(p.Merged) != 2 {
		t.Fatalf("%+v", p)
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(p.Path, f)); err != nil {
			t.Errorf("%s missing after merge", f)
		}
	}
	if p.HeadSHA != revParse(t, p.Path, "HEAD") {
		t.Error("HeadSHA is not the worktree HEAD")
	}
	// Merge commits are --no-ff, by the configured identity.
	if parents := strings.Fields(gitOut(t, p.Path, "log", "-1", "--format=%P")); len(parents) != 2 {
		t.Errorf("last merge has %d parents, want 2", len(parents))
	}
	if who := strings.TrimSpace(gitOut(t, p.Path, "log", "-1", "--format=%an <%ae>")); who != "Test User <test@example.com>" {
		t.Errorf("merge author = %q", who)
	}
}

// Repeating the same merge on the re-attached branch is a no-op.
func TestMergeBranches_AlreadyMergedIsNoop(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	branchWith(t, repo, runBranch("n1"), "main", "a.txt", "a\n")
	branchWith(t, repo, runBranch("n2"), "main", "b.txt", "b\n")
	list := []string{runBranch("n1"), runBranch("n2")}
	p, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main", list)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main", list)
	if err != nil || !p2.Reattached || p2.Conflict != nil || len(p2.Merged) != 2 || p2.HeadSHA != p.HeadSHA {
		t.Errorf("repeat: %+v, %v (head was %s)", p2, err, p.HeadSHA)
	}
	if n := strings.TrimSpace(gitOut(t, p.Path, "rev-list", "--count", "main..HEAD")); n != "4" {
		t.Errorf("%s commits past main after the repeat, want the original 4 (two nodes, two merges)", n)
	}
}

// A conflicting merge is aborted, and git cannot always give back local
// changes it set aside for it: a dirty tree is refused up front.
func TestMergeBranches_RefusesDirtyWorktree(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	branchWith(t, repo, runBranch("n1"), "main", "same.txt", "one\n")
	p, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(p.Path, "same.txt"), "my hand edit\n")
	_, err = PrepareRun(ctx, repo, run, runBranch("merge"), "main", []string{runBranch("n1")})
	if !errors.Is(err, ErrDirty) {
		t.Fatalf("err = %v, want ErrDirty", err)
	}
	if b, _ := os.ReadFile(filepath.Join(p.Path, "same.txt")); string(b) != "my hand edit\n" {
		t.Errorf("the edit was lost: %q", b)
	}
	// Hive's own agent-config links are not the user's work.
	mustMkdir(t, filepath.Join(repo, ".claude", "skills"))
	clean, err := PrepareRun(ctx, repo, run, runBranch("m2"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	LinkAgentConfig(repo, clean.Path)
	if _, err := PrepareRun(ctx, repo, run, runBranch("m2"), "main", []string{runBranch("n1")}); err != nil {
		t.Errorf("linked agent config counted as dirty: %v", err)
	}
}

func TestPrepareWorktree_MergeListIsBounded(t *testing.T) {
	repo := workflowRepo(t)
	many := make([]string, maxMergeBranches+1)
	for i := range many {
		many[i] = runBranch("n")
	}
	if _, err := PrepareRun(context.Background(), repo, run, runBranch("m"), "main", many); !errors.Is(err, ErrInvalidWorkflowBranch) {
		t.Errorf("err = %v, want a refusal", err)
	}
}

// A worktree path is attacker-chosen text to the parser: a newline in it
// must not read as a forged `branch` line.
func TestFindBranchWorktree_PathWithNewlineCannotForgeABranch(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	sha := revParse(t, repo, "HEAD")
	evil := filepath.Join(t.TempDir(), "evil\nbranch refs/heads/"+runBranch("forged"))
	if err := AddNewBranch(ctx, repo, runBranch("real"), evil, sha); err != nil {
		t.Skipf("this filesystem or git refuses a newline in a path: %v", err)
	}
	if _, found, err := FindBranchWorktree(ctx, repo, runBranch("forged")); err != nil || found {
		t.Errorf("forged branch found=%v err=%v", found, err)
	}
	wt, found, err := FindBranchWorktree(ctx, repo, runBranch("real"))
	if err != nil || !found || wt.Path != ResolvePath(evil) {
		t.Errorf("real branch: %+v found=%v err=%v", wt, found, err)
	}
}

func TestParseWorktreeListZ(t *testing.T) {
	out := "worktree /a\x00HEAD aaa\x00branch refs/heads/main\x00\x00" +
		"worktree /b\nbranch refs/heads/x\x00HEAD bbb\x00detached\x00prunable gone\x00\x00"
	got := parseWorktreeListZ(out)
	if len(got) != 2 || got[0].Branch != "main" || got[1].Branch != "" || !got[1].Detached || !got[1].Prunable {
		t.Fatalf("%+v", got)
	}
	if got[1].Path != ResolvePath("/b\nbranch refs/heads/x") {
		t.Errorf("path = %q", got[1].Path)
	}
}

// An agent can rewrite the .git file in its own worktree to point at a
// gitdir it controls (filters, hooks). It must be refused, not followed.
func TestCommitWorktree_RefusesRedirectedGitfile(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "pwned")
	evil := filepath.Join(p.Path, ".evil")
	mustGit(t, p.Path, "init", "-q", evil)
	mustGit(t, evil, "config", "filter.x.clean", "sh -c 'touch "+marker+"; cat'")
	mustWrite(t, filepath.Join(p.Path, ".gitattributes"), "* filter=x\n")
	mustWrite(t, filepath.Join(p.Path, "f"), "f")
	mustWrite(t, filepath.Join(p.Path, ".git"), "gitdir: "+filepath.Join(evil, ".git")+"\n")
	if _, _, err := CommitRun(ctx, repo, run, runBranch("a"), "m"); !errors.Is(err, ErrNotManaged) {
		t.Errorf("err = %v, want ErrNotManaged", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a filter from the redirected gitdir ran")
	}
}

// Config-defined hooks (git 2.54+) run beside the hooks directory.
func TestSafeGit_ConfigHooksDoNotRun(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	mustGit(t, repo, "config", "hook.x.event", "post-commit")
	mustGit(t, repo, "config", "hook.x.command", "touch '"+marker+"'")
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(p.Path, "f"), "f")
	if _, _, err := CommitRun(ctx, repo, run, runBranch("a"), "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a config-defined hook ran")
	}
}

func TestCommitWorktree_HooksDoNotRun(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	markers := t.TempDir()
	for _, h := range []string{"pre-commit", "commit-msg", "post-commit", "prepare-commit-msg", "post-checkout"} {
		script := "#!/bin/sh\ntouch '" + filepath.Join(markers, h) + "'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", h), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(p.Path, "f"), "f")
	if committed, _, err := CommitRun(ctx, repo, run, runBranch("a"), "m"); err != nil || !committed {
		t.Fatalf("committed=%v err=%v (a failing hook must not matter)", committed, err)
	}
	if entries, _ := os.ReadDir(markers); len(entries) != 0 {
		t.Errorf("hooks ran: %v", entries)
	}
}

func TestMergeBranches_ConflictReportsBranchAndFiles(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	branchWith(t, repo, runBranch("n1"), "main", "dir/a file.txt", "one\n")
	branchWith(t, repo, runBranch("n2"), "main", "dir/a file.txt", "two\n")
	branchWith(t, repo, runBranch("n3"), "main", "c.txt", "c\n")
	p, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main",
		[]string{runBranch("n1"), runBranch("n2"), runBranch("n3")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Conflict == nil || p.Conflict.Branch != runBranch("n2") || len(p.Conflict.Files) != 1 || p.Conflict.Files[0] != "dir/a file.txt" {
		t.Fatalf("conflict = %+v", p.Conflict)
	}
	if len(p.Merged) != 1 || p.Merged[0] != runBranch("n1") {
		t.Errorf("merged = %v, want only n1", p.Merged)
	}
	if st := strings.TrimSpace(gitOut(t, p.Path, "status", "--porcelain")); st != "" {
		t.Errorf("tree not clean after the abort:\n%s", st)
	}
	if _, err := os.Stat(filepath.Join(p.Path, "c.txt")); err == nil {
		t.Error("merging continued past the conflict")
	}
	// The worktree is kept, so the user can resolve by hand.
	if _, err := os.Stat(p.Path); err != nil {
		t.Error("the worktree was removed")
	}
}

func TestMergeBranches_NoIdentity(t *testing.T) {
	repo := initRepo(t) // no local identity
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, v := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	ctx := context.Background()
	branchWith(t, repo, runBranch("n1"), "main", "a.txt", "a\n")
	_, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main", []string{runBranch("n1")})
	if !errors.Is(err, ErrIdentityMissing) {
		t.Fatalf("err = %v, want ErrIdentityMissing", err)
	}
}

func TestSafeGit_HooksAndFsmonitorOff(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	markers := t.TempDir()
	hook := func(name string) {
		script := "#!/bin/sh\ntouch '" + filepath.Join(markers, name) + "'\n"
		path := filepath.Join(repo, ".git", "hooks", name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []string{"post-checkout", "pre-merge-commit", "post-merge", "pre-commit", "commit-msg", "post-commit", "reference-transaction"} {
		hook(h)
	}
	fsm := filepath.Join(markers, "fsmonitor")
	fsmScript := filepath.Join(t.TempDir(), "fsm.sh")
	if err := os.WriteFile(fsmScript, []byte("#!/bin/sh\ntouch '"+fsm+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "config", "core.fsmonitor", fsmScript)
	// The branches themselves are built with hooks off too.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", t.TempDir())
	branchWith(t, repo, runBranch("n1"), "main", "a.txt", "a\n")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	entries, _ := os.ReadDir(markers)
	for _, e := range entries { // anything the setup above triggered
		_ = os.Remove(filepath.Join(markers, e.Name()))
	}

	p, err := PrepareRun(ctx, repo, run, runBranch("merge"), "main", []string{runBranch("n1")})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(p.Path, "x.txt"), "x\n")
	if _, _, err := CommitRun(ctx, repo, run, runBranch("merge"), "msg"); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(markers)
	for _, e := range entries {
		t.Errorf("%s ran during the workflow git calls", e.Name())
	}
}

func TestCommitWorktree_CommitsAll(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(p.Path, "new.txt"), "n\n")
	mustMkdir(t, filepath.Join(p.Path, "sub"))
	mustWrite(t, filepath.Join(p.Path, "sub", "deep.txt"), "d\n")
	committed, head, err := CommitRun(ctx, repo, run, runBranch("a"), "-S --amend: work")
	if err != nil || !committed {
		t.Fatalf("committed=%v err=%v", committed, err)
	}
	if head != revParse(t, p.Path, "HEAD") {
		t.Error("head is not the worktree HEAD")
	}
	if got := strings.TrimSpace(gitOut(t, p.Path, "log", "-1", "--format=%s")); got != "-S --amend: work" {
		t.Errorf("subject = %q (a message must never be read as an option)", got)
	}
	if got := strings.TrimSpace(gitOut(t, p.Path, "show", "--name-only", "--format=", "HEAD")); got != "new.txt\nsub/deep.txt" {
		t.Errorf("committed files:\n%s", got)
	}
	if who := strings.TrimSpace(gitOut(t, p.Path, "log", "-1", "--format=%an <%ae>")); who != "Test User <test@example.com>" {
		t.Errorf("author = %q", who)
	}
}

func TestCommitWorktree_CleanIsNoop(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	committed, head, err := CommitRun(ctx, repo, run, runBranch("a"), "nothing")
	if err != nil || committed || head != p.HeadSHA {
		t.Errorf("clean tree: committed=%v head=%s err=%v (want no-op at %s)", committed, head, err, p.HeadSHA)
	}
}

func TestCommitWorktree_SkipsLinkedAgentConfig(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	mustMkdir(t, filepath.Join(repo, ".claude", "skills"))
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	LinkAgentConfig(repo, p.Path)
	if _, err := os.Lstat(filepath.Join(p.Path, ".claude", "skills")); err != nil {
		t.Skip("symlink not created on this platform")
	}
	committed, _, err := CommitRun(ctx, repo, run, runBranch("a"), "m")
	if err != nil || committed {
		t.Errorf("hive's own agent-config link was committed: committed=%v err=%v", committed, err)
	}
}

func TestCommitWorktree_RefusesNonRunBranch(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	if _, _, err := CommitRun(ctx, repo, run, "main", "m"); !errors.Is(err, ErrInvalidWorkflowBranch) {
		t.Errorf("non-run branch: %v", err)
	}
	if _, _, err := CommitRun(ctx, repo, run, runBranch("none"), "m"); !errors.Is(err, ErrNotManaged) {
		t.Errorf("branch with no worktree: %v", err)
	}
	if _, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{"", "a\x00b", strings.Repeat("x", maxCommitMessage+1)} {
		if _, _, err := CommitRun(ctx, repo, run, runBranch("a"), msg); err == nil {
			t.Errorf("message of %d bytes accepted", len(msg))
		}
	}
}

// A clean tree is a no-op even with no identity; only a real commit needs one.
func TestCommitWorktree_CleanNeedsNoIdentity(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "config", "--unset", "user.name")
	mustGit(t, repo, "config", "--unset", "user.email")
	for _, v := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	if committed, head, err := CommitRun(ctx, repo, run, runBranch("a"), "m"); err != nil || committed || head != p.HeadSHA {
		t.Errorf("committed=%v head=%s err=%v", committed, head, err)
	}
}

// Merge options in the repo config cannot turn a merge into a staged,
// uncommitted one.
func TestMergeBranches_IgnoresNoCommitConfig(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	branchWith(t, repo, runBranch("n1"), "main", "a.txt", "a\n")
	mustGit(t, repo, "config", "branch.hive/wf/r1/m.mergeOptions", "--no-commit")
	mustGit(t, repo, "config", "merge.ff", "only")
	p, err := PrepareRun(ctx, repo, run, runBranch("m"), "main", []string{runBranch("n1")})
	if err != nil || p.Conflict != nil {
		t.Fatal(err)
	}
	if st := strings.TrimSpace(gitOut(t, p.Path, "status", "--porcelain")); st != "" {
		t.Errorf("merge left a pending state:\n%s", st)
	}
	if parents := strings.Fields(gitOut(t, p.Path, "log", "-1", "--format=%P")); len(parents) != 2 {
		t.Errorf("not a merge commit: %d parents", len(parents))
	}
}

func TestCommitWorktree_NoIdentity(t *testing.T) {
	repo := workflowRepo(t)
	ctx := context.Background()
	p, err := PrepareRun(ctx, repo, run, runBranch("a"), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(p.Path, "f"), "f")
	mustGit(t, repo, "config", "--unset", "user.name")
	mustGit(t, repo, "config", "--unset", "user.email")
	for _, v := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	if _, _, err := CommitRun(ctx, repo, run, runBranch("a"), "m"); !errors.Is(err, ErrIdentityMissing) {
		t.Errorf("err = %v, want ErrIdentityMissing", err)
	}
}

func TestSafeGit_ScrubsRedirectingEnv(t *testing.T) {
	repo := workflowRepo(t)
	other := workflowRepo(t)
	mustGit(t, other, "commit", "--allow-empty", "-q", "-m", "other")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	got, err := ResolveCommit(context.Background(), repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// The two repos have different init commits; resolving in `repo`
	// proves GIT_DIR did not redirect it.
	os.Unsetenv("GIT_DIR")
	os.Unsetenv("GIT_WORK_TREE")
	if want := revParse(t, repo, "HEAD"); got != want {
		t.Errorf("resolved %s, want %s", got, want)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
