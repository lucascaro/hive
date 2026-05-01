package worktree

import (
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
		"feature/x":  "feature-x",
		"hot fix":    "hot-fix",
		"a:b":        "a-b",
		"win\\path":  "win-path",
		"plain":      "plain",
	}
	for in, wantSeg := range cases {
		got := WorktreePath("/r", in)
		want := filepath.Join("/r", ".worktrees", wantSeg)
		if got != want {
			t.Errorf("WorktreePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	branch := "feature-x"
	wtPath := WorktreePath(repo, branch)
	if err := CreateWorktree(repo, branch, wtPath); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree dir missing after Create: %v", err)
	}
	if err := RemoveWorktree(repo, wtPath); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wtPath); err == nil {
		t.Errorf("worktree dir still exists after Remove")
	}
}

func TestCreateWorktree_BranchAlreadyExists(t *testing.T) {
	skipNoGit(t)
	repo := initRepo(t)
	// Create a branch up front so the -b path collides.
	if out, err := exec.Command("git", "-C", repo, "branch", "topic").CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	wt := WorktreePath(repo, "topic")
	if err := CreateWorktree(repo, "topic", wt); err != nil {
		t.Fatalf("CreateWorktree should fall back when branch exists: %v", err)
	}
	defer Cleanup(repo, wt)
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
	if err := CreateWorktree(repo, "wip", wt); err != nil {
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

func TestIsInGitignoreAndAdd(t *testing.T) {
	dir := t.TempDir()
	if IsInGitignore(dir, ".worktrees") {
		t.Errorf("missing .gitignore reported as containing pattern")
	}
	if err := AddToGitignore(dir, ".worktrees"); err != nil {
		t.Fatalf("AddToGitignore: %v", err)
	}
	if !IsInGitignore(dir, ".worktrees") {
		t.Errorf("just-added pattern not detected")
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
