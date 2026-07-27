package worktree

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func skipNoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("worktree tests require POSIX shell git")
	}
}

// initRepo creates a fresh git repo with one commit so that HEAD is
// valid (a prereq for `git worktree add`).
func initRepo(t *testing.T) string {
	t.Helper()
	skipNoGit(t)
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestIsGitRepoAndRoot(t *testing.T) {
	skipNoGit(t)
	dir := initRepo(t)
	if !IsGitRepo(dir) {
		t.Errorf("IsGitRepo(%q) = false, want true", dir)
	}
	root, err := Root(dir)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	// Resolve symlinks (macOS /private/var vs /var).
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(root)
	if got != want {
		t.Errorf("Root = %q, want %q", got, want)
	}

	notRepo := t.TempDir()
	if IsGitRepo(notRepo) {
		t.Errorf("IsGitRepo on non-repo dir = true")
	}
}

func TestWorktreePathSanitizes(t *testing.T) {
	cases := map[string]string{
		"feature/x": "feature-x",
		"hot fix":   "hot-fix",
		"a:b":       "a-b",
		"win\\path": "win-path",
		"plain":     "plain",
	}
	for in, wantSeg := range cases {
		got := WorktreePath("/r", in)
		want := filepath.Join("/r", ".worktrees", wantSeg)
		if got != want {
			t.Errorf("WorktreePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// initRepoWithUpstream creates a bare "upstream" repo with one commit,
// clones it locally, then advances the bare repo by another commit so
// that the local clone is one commit behind origin/main. Returns the
// local clone path, the local-main HEAD sha (stale), and the
// origin/main HEAD sha (fresh).
func initRepoWithUpstream(t *testing.T) (localRepo, staleSHA, freshSHA string) {
	t.Helper()
	skipNoGit(t)

	// 1. Bare upstream.
	upstream := t.TempDir()
	mustGit(t, upstream, "init", "-q", "--bare", "-b", "main")

	// 2. Seed upstream with one commit via a throwaway worktree.
	seed := t.TempDir()
	mustGit(t, seed, "init", "-q", "-b", "main")
	mustGit(t, seed, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "--allow-empty", "-q", "-m", "seed")
	mustGit(t, seed, "remote", "add", "origin", upstream)
	mustGit(t, seed, "push", "-q", "origin", "main")

	// 3. Clone upstream as the local repo.
	parent := t.TempDir()
	local := filepath.Join(parent, "repo")
	mustGit(t, parent, "clone", "-q", upstream, local)
	mustGit(t, local, "-c", "user.email=t@t", "-c", "user.name=t",
		"config", "user.email", "t@t")
	mustGit(t, local, "config", "user.name", "t")
	staleSHA = revParse(t, local, "HEAD")

	// 4. Advance upstream by one commit (so local is now behind).
	seed2 := t.TempDir()
	mustGit(t, parent, "clone", "-q", upstream, filepath.Join(seed2, "wt"))
	wt := filepath.Join(seed2, "wt")
	mustGit(t, wt, "config", "user.email", "t@t")
	mustGit(t, wt, "config", "user.name", "t")
	mustGit(t, wt, "commit", "--allow-empty", "-q", "-m", "advance")
	mustGit(t, wt, "push", "-q", "origin", "main")
	freshSHA = revParseRemote(t, upstream, "main")

	localRepo = local
	return localRepo, staleSHA, freshSHA
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func revParseRemote(t *testing.T, bareRepo, branch string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", bareRepo, "rev-parse", branch).Output()
	if err != nil {
		t.Fatalf("rev-parse %s in %s: %v", branch, bareRepo, err)
	}
	return strings.TrimSpace(string(out))
}

// TestCreateWorktree_CancelledContext pins the cancellation contract
// added when the git helpers stopped rooting their own
// context.Background() timeouts: a cancelled caller ctx must abort the
// add and leave no worktree behind.
func TestCreateWorktree_CancelledContext(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wtPath := WorktreePath(repo, "cancelled")
	if err := CreateWorktree(ctx, repo, "cancelled", wtPath); err == nil {
		t.Fatal("CreateWorktree with a cancelled ctx: want error, got nil")
	}
	if _, err := os.Stat(wtPath); err == nil {
		t.Errorf("worktree dir %s exists after a cancelled create", wtPath)
	}
}

func TestCreateWorktree_PrefersUpstreamBase(t *testing.T) {
	local, stale, fresh := initRepoWithUpstream(t)
	if stale == fresh {
		t.Fatalf("test setup failed: stale == fresh sha")
	}

	branch := "feature-x"
	wtPath := WorktreePath(local, branch)
	if err := CreateWorktree(context.Background(), local, branch, wtPath); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer Cleanup(local, wtPath)

	got := revParse(t, wtPath, "HEAD")
	if got != fresh {
		t.Errorf("worktree HEAD = %s, want origin/main %s (stale local main was %s)", got, fresh, stale)
	}
}

func TestCreateWorktree_NoRemoteFallsBackToHEAD(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	headBefore := revParse(t, repo, "HEAD")

	branch := "no-remote-feature"
	wtPath := WorktreePath(repo, branch)
	if err := CreateWorktree(context.Background(), repo, branch, wtPath); err != nil {
		t.Fatalf("CreateWorktree on repo without remote: %v", err)
	}
	defer Cleanup(repo, wtPath)

	got := revParse(t, wtPath, "HEAD")
	if got != headBefore {
		t.Errorf("worktree HEAD = %s, want local HEAD %s", got, headBefore)
	}
}

func TestCreateWorktree_UnreachableRemoteWarnsAndFallsBack(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	// Point origin at a path that does not exist. `git fetch` will fail;
	// `symbolic-ref refs/remotes/origin/HEAD` will also fail (never set).
	bogus := filepath.Join(t.TempDir(), "does-not-exist.git")
	mustGit(t, repo, "remote", "add", "origin", bogus)

	var buf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOut) })

	headBefore := revParse(t, repo, "HEAD")
	branch := "offline-feature"
	wtPath := WorktreePath(repo, branch)
	if err := CreateWorktree(context.Background(), repo, branch, wtPath); err != nil {
		t.Fatalf("CreateWorktree with unreachable remote: %v", err)
	}
	defer Cleanup(repo, wtPath)

	if got := revParse(t, wtPath, "HEAD"); got != headBefore {
		t.Errorf("worktree HEAD = %s, want local HEAD %s (fallback path)", got, headBefore)
	}
	logs := buf.String()
	if !strings.Contains(logs, "worktree:") {
		t.Errorf("expected worktree warning in logs when remote is unreachable; got: %q", logs)
	}
	// Must mention either the fetch failure or the missing origin/HEAD so
	// the operator can diagnose stale-upstream risk.
	if !strings.Contains(logs, "fetch origin failed") && !strings.Contains(logs, "origin/HEAD not set") {
		t.Errorf("expected log to mention fetch failure or missing origin/HEAD; got: %q", logs)
	}
}

func TestCreateWorktree_BranchAlreadyExists(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	// Create a branch up front so the -b path collides.
	if out, err := exec.Command("git", "-C", repo, "branch", "topic").CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	topicSHA := revParse(t, repo, "topic")
	wt := WorktreePath(repo, "topic")
	if err := CreateWorktree(context.Background(), repo, "topic", wt); err != nil {
		t.Fatalf("CreateWorktree should check out an existing branch: %v", err)
	}
	defer Cleanup(repo, wt)
	// The worktree must actually be on the existing branch's tip — the
	// rev-parse probe (not git's localized error text) routes us here.
	if got := revParse(t, wt, "HEAD"); got != topicSHA {
		t.Errorf("worktree HEAD = %s, want topic tip %s", got, topicSHA)
	}
}

func TestCreateWorktree_ExistingBranchCheckedOutElsewhereReportsContext(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	mustGit(t, repo, "branch", "topic")
	wtA := WorktreePath(repo, "topic")
	if err := CreateWorktree(context.Background(), repo, "topic", wtA); err != nil {
		t.Fatalf("first CreateWorktree: %v", err)
	}
	defer Cleanup(repo, wtA)

	// A second worktree for the same branch must fail (git refuses), and
	// the error must say which strategy failed instead of a bare git dump.
	wtB := filepath.Join(repo, ".worktrees", "topic-second")
	err := CreateWorktree(context.Background(), repo, "topic", wtB)
	if err == nil {
		t.Fatal("second CreateWorktree for same branch should fail")
	}
	if !strings.Contains(err.Error(), "existing branch topic") {
		t.Errorf("error should name the existing-branch strategy; got: %v", err)
	}
}

func TestCreateWorktree_AllAttemptsFailJoinsErrors(t *testing.T) {
	repo, _, _ := initRepoWithUpstream(t)
	// ".." is invalid in ref names, so both the with-base and no-base
	// attempts fail. The joined error must carry context from each.
	bad := "bad..name"
	err := CreateWorktree(context.Background(), repo, bad, filepath.Join(repo, ".worktrees", "bad-name"))
	if err == nil {
		t.Fatal("CreateWorktree with invalid branch name should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, `(base "origin/main")`) {
		t.Errorf("error should report the upstream-base attempt; got: %v", err)
	}
	if !strings.Contains(msg, "(no base)") {
		t.Errorf("error should report the no-base retry attempt; got: %v", err)
	}
}

func TestCleanup_MissingDir(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	wt := filepath.Join(repo, ".worktrees", "never-existed")
	// Cleanup should be tolerant: prune succeeds, remove returns
	// best-effort wrapped error or nil.
	if err := Cleanup(repo, wt); err != nil {
		// Acceptable: the underlying `git worktree remove` on a
		// missing path may surface; what matters is it doesn't panic
		// and prune ran.
		t.Logf("Cleanup on missing dir surfaced expected non-fatal error: %v", err)
	}
}

func TestHasUncommitted(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	wt := WorktreePath(repo, "wip")
	if err := CreateWorktree(context.Background(), repo, "wip", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer Cleanup(repo, wt)

	dirty, err := HasUncommitted(wt)
	if err != nil {
		t.Fatalf("HasUncommitted clean: %v", err)
	}
	if dirty {
		t.Errorf("clean worktree reported as dirty")
	}

	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	dirty, err = HasUncommitted(wt)
	if err != nil {
		t.Fatalf("HasUncommitted dirty: %v", err)
	}
	if !dirty {
		t.Errorf("worktree with untracked file reported as clean")
	}
}

func TestHasUncommitted_MissingDir(t *testing.T) {
	dirty, err := HasUncommitted(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Errorf("expected nil error for missing dir, got %v", err)
	}
	if dirty {
		t.Errorf("missing dir reported as dirty")
	}
}

func TestAddToGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := AddToGitignore(dir, ".worktrees"); err != nil {
		t.Fatalf("AddToGitignore: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), ".worktrees") {
		t.Errorf("gitignore missing pattern; got %q", data)
	}
}

func TestEnsureGitignore_NoFile(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	// No .gitignore present.
	EnsureGitignore(repo)
	if _, err := os.Stat(filepath.Join(repo, ".gitignore")); err == nil {
		t.Errorf("EnsureGitignore created .gitignore from scratch (it should not)")
	}
}

func TestEnsureGitignore_AppendsWhenMissing(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("dist/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	EnsureGitignore(repo)
	body, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if !strings.Contains(string(body), ".worktrees") {
		t.Errorf("EnsureGitignore did not append .worktrees: %q", body)
	}
}

func TestEnsureGitignore_AlreadyCovered(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".worktrees\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	EnsureGitignore(repo)
	body, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	// We added one line; ensure we didn't double-add.
	if strings.Count(string(body), ".worktrees") != 1 {
		t.Errorf("EnsureGitignore double-added when pattern already present: %q", body)
	}
}

func TestResolveBranchAndPath(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)

	// 1. Empty request → random name; path doesn't exist yet.
	branch, path, err := ResolveBranchAndPath(repo, "")
	if err != nil {
		t.Fatalf("ResolveBranchAndPath empty: %v", err)
	}
	if branch == "" {
		t.Errorf("empty branch returned")
	}
	if path != WorktreePath(repo, branch) {
		t.Errorf("path mismatch: got %q want %q", path, WorktreePath(repo, branch))
	}

	// 2. Collision: pre-create the dir, ResolveBranchAndPath should suffix.
	if err := os.MkdirAll(WorktreePath(repo, "fixed"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	branch, path, err = ResolveBranchAndPath(repo, "fixed")
	if err != nil {
		t.Fatalf("ResolveBranchAndPath collision: %v", err)
	}
	if branch != "fixed-2" {
		t.Errorf("expected fixed-2, got %q", branch)
	}
	if filepath.Base(path) != "fixed-2" {
		t.Errorf("path base = %q, want fixed-2", filepath.Base(path))
	}
}

func TestRandomBranchName(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		n := RandomBranchName()
		if !strings.Contains(n, "-") {
			t.Errorf("name missing dash: %q", n)
		}
		seen[n] = true
	}
	if len(seen) < 2 {
		t.Errorf("RandomBranchName returned the same value 20 times")
	}
}

// LinkAgentConfig must link the untracked config git left behind while
// leaving anything already in the worktree (tracked files git checked
// out) untouched.
func TestLinkAgentConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	repo, wt := t.TempDir(), t.TempDir()

	// .claude exists in both: tracked settings.json already checked out,
	// skills/ untracked and missing from the worktree.
	mustMkdir(t, filepath.Join(repo, ".claude", "skills"))
	mustWrite(t, filepath.Join(repo, ".claude", "skills", "s.md"), "skill")
	mustWrite(t, filepath.Join(repo, ".claude", "settings.json"), "main")
	mustMkdir(t, filepath.Join(wt, ".claude"))
	mustWrite(t, filepath.Join(wt, ".claude", "settings.json"), "checked-out")

	// .agents missing from the worktree entirely.
	mustMkdir(t, filepath.Join(repo, ".agents", "skills"))

	// Per-checkout state must NOT be shared across worktrees.
	mustWrite(t, filepath.Join(repo, ".claude", "scheduled_tasks.lock"), "pid")

	LinkAgentConfig(repo, wt)

	// Tracked file untouched — not clobbered, not replaced by a symlink.
	got, err := os.ReadFile(filepath.Join(wt, ".claude", "settings.json"))
	if err != nil || string(got) != "checked-out" {
		t.Errorf("settings.json = %q, %v; want %q", got, err, "checked-out")
	}
	if fi, err := os.Lstat(filepath.Join(wt, ".claude", "settings.json")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("settings.json became a symlink")
	}

	// Missing child linked through to the main checkout.
	if got, err := os.ReadFile(filepath.Join(wt, ".claude", "skills", "s.md")); err != nil || string(got) != "skill" {
		t.Errorf("skills/s.md = %q, %v; want %q", got, err, "skill")
	}
	// Config dir absent from the worktree is created and populated.
	if fi, err := os.Lstat(filepath.Join(wt, ".agents", "skills")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".agents/skills = %v, %v; want symlink", fi, err)
	}
	// Non-allowlisted state left behind.
	if _, err := os.Lstat(filepath.Join(wt, ".claude", "scheduled_tasks.lock")); err == nil {
		t.Errorf("scheduled_tasks.lock was linked; per-checkout state must not be shared")
	}

	// Idempotent: a second call must not error or duplicate.
	LinkAgentConfig(repo, wt)
	if got, err := os.ReadFile(filepath.Join(wt, ".claude", "settings.json")); err != nil || string(got) != "checked-out" {
		t.Errorf("second call disturbed settings.json: %q, %v", got, err)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Cleanup must not delete through the symlinks LinkAgentConfig created
// — the targets are the user's real config in the main checkout.
func TestCleanupDoesNotFollowAgentConfigLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	repo := initRepo(t)
	sentinel := filepath.Join(repo, ".claude", "skills", "s.md")
	mustMkdir(t, filepath.Dir(sentinel))
	mustWrite(t, sentinel, "skill")

	wt := filepath.Join(repo, ".worktrees", "wt")
	if err := CreateWorktree(context.Background(), repo, "wt", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	LinkAgentConfig(repo, wt)
	if _, err := os.Lstat(filepath.Join(wt, ".claude", "skills")); err != nil {
		t.Fatalf("link not created: %v", err)
	}

	if err := Cleanup(repo, wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "skill" {
		t.Fatalf("Cleanup destroyed the real config: %q, %v", got, err)
	}
}

// The pathspec exclusions in HasUncommitted must hide only the entries
// LinkAgentConfig planted — genuine work still has to register, or the
// dirty check stops protecting anything.
func TestHasUncommittedIgnoresLinkedConfigOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	repo := initRepo(t)
	mustMkdir(t, filepath.Join(repo, ".claude", "skills"))
	mustWrite(t, filepath.Join(repo, ".claude", "skills", "s.md"), "skill")

	wt := filepath.Join(repo, ".worktrees", "wt")
	if err := CreateWorktree(context.Background(), repo, "wt", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	LinkAgentConfig(repo, wt)

	if dirty, err := HasUncommitted(wt); err != nil || dirty {
		t.Errorf("linked config alone reads dirty: %v, %v", dirty, err)
	}

	// A real untracked file beside the links must still count.
	mustWrite(t, filepath.Join(wt, "notes.md"), "work")
	if dirty, err := HasUncommitted(wt); err != nil || !dirty {
		t.Errorf("real untracked file not reported: %v, %v", dirty, err)
	}

	// So must a real file inside a config dir that isn't a linked entry.
	if err := os.Remove(filepath.Join(wt, "notes.md")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(wt, ".claude", "notes.md"), "work")
	if dirty, err := HasUncommitted(wt); err != nil || !dirty {
		t.Errorf("real file inside .claude not reported: %v, %v", dirty, err)
	}
}

// copyEntry is LinkAgentConfig's fallback where symlinks are
// unavailable (Windows without elevation). Exercised directly so it is
// covered on the platforms CI actually runs.
func TestCopyEntry(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()

	// Directory, including a nested file.
	mustMkdir(t, filepath.Join(src, "skills", "nested"))
	mustWrite(t, filepath.Join(src, "skills", "nested", "s.md"), "skill")
	if err := copyEntry(filepath.Join(src, "skills"), filepath.Join(dst, "skills")); err != nil {
		t.Fatalf("copyEntry dir: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "skills", "nested", "s.md")); err != nil || string(got) != "skill" {
		t.Errorf("nested file = %q, %v; want %q", got, err, "skill")
	}

	// Single file.
	mustWrite(t, filepath.Join(src, "settings.local.json"), "{}")
	if err := copyEntry(filepath.Join(src, "settings.local.json"), filepath.Join(dst, "settings.local.json")); err != nil {
		t.Fatalf("copyEntry file: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "settings.local.json")); err != nil || string(got) != "{}" {
		t.Errorf("file = %q, %v; want %q", got, err, "{}")
	}

	// Missing source is an error, not a silent empty copy.
	if err := copyEntry(filepath.Join(src, "nope"), filepath.Join(dst, "nope")); err == nil {
		t.Errorf("copyEntry on missing source returned nil")
	}
}

// The dirty check must not go blind to real work sitting at an
// allowlisted path. Two ways that happens, both data loss if missed:
// a project that COMMITS its agent config (LinkAgentConfig skips those
// paths, so they were never ours), and a user replacing one of our
// links with real local content.
func TestHasUncommittedSeesRealWorkAtLinkedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	repo := initRepo(t)
	// Committed commands/ — plenty of projects share these in-repo.
	mustMkdir(t, filepath.Join(repo, ".claude", "commands"))
	mustWrite(t, filepath.Join(repo, ".claude", "commands", "c.md"), "committed")
	mustMkdir(t, filepath.Join(repo, ".claude", "skills"))
	mustWrite(t, filepath.Join(repo, ".claude", "skills", "s.md"), "skill")
	for _, args := range [][]string{
		{"add", "-A", ".claude/commands"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "commands"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	wt := filepath.Join(repo, ".worktrees", "wt")
	if err := CreateWorktree(context.Background(), repo, "wt", wt); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	LinkAgentConfig(repo, wt)
	if dirty, err := HasUncommitted(wt); err != nil || dirty {
		t.Fatalf("worktree dirty before any real edit: %v, %v", dirty, err)
	}

	// A tracked file under an allowlisted path — never linked, since git
	// already checked it out — must still register when edited.
	mustWrite(t, filepath.Join(wt, ".claude", "commands", "c.md"), "my real work")
	if dirty, err := HasUncommitted(wt); err != nil || !dirty {
		t.Errorf("edit to a committed allowlisted file reads clean: %v, %v", dirty, err)
	}
	mustWrite(t, filepath.Join(wt, ".claude", "commands", "c.md"), "committed")

	// Replacing one of our links with real content must register too.
	linked := filepath.Join(wt, ".claude", "skills")
	if fi, err := os.Lstat(linked); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("precondition: %s is not a symlink (%v, %v)", linked, fi, err)
	}
	if err := os.Remove(linked); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, linked)
	mustWrite(t, filepath.Join(linked, "mine.md"), "my real work")
	if dirty, err := HasUncommitted(wt); err != nil || !dirty {
		t.Errorf("real content replacing a linked entry reads clean: %v, %v", dirty, err)
	}
}
