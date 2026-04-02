package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/git"
)

// initRepo creates a temporary git repository for testing.
// It also makes an initial commit so the repo is non-empty (required for worktrees).
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")
	// Create a file and commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestIsGitRepo(t *testing.T) {
	t.Run("inside repo", func(t *testing.T) {
		dir := initRepo(t)
		if !git.IsGitRepo(dir) {
			t.Errorf("IsGitRepo(%q) = false, want true", dir)
		}
	})

	t.Run("not a repo", func(t *testing.T) {
		dir := t.TempDir()
		if git.IsGitRepo(dir) {
			t.Errorf("IsGitRepo(%q) = true, want false", dir)
		}
	})

	t.Run("subdirectory of repo", func(t *testing.T) {
		dir := initRepo(t)
		sub := filepath.Join(dir, "sub", "dir")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		if !git.IsGitRepo(sub) {
			t.Errorf("IsGitRepo(%q) = false, want true (subdir of repo)", sub)
		}
	})
}

func TestRoot(t *testing.T) {
	t.Run("at root", func(t *testing.T) {
		dir := initRepo(t)
		got, err := git.Root(dir)
		if err != nil {
			t.Fatalf("Root(%q) error: %v", dir, err)
		}
		// On macOS, TempDir may use a symlink under /var; resolve both.
		if filepath.Base(got) != filepath.Base(dir) {
			t.Errorf("Root(%q) = %q, want base %q", dir, got, filepath.Base(dir))
		}
	})

	t.Run("from subdirectory", func(t *testing.T) {
		dir := initRepo(t)
		sub := filepath.Join(dir, "a", "b")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		got, err := git.Root(sub)
		if err != nil {
			t.Fatalf("Root(%q) error: %v", sub, err)
		}
		if filepath.Base(got) != filepath.Base(dir) {
			t.Errorf("Root(%q) = %q, want base %q", sub, got, filepath.Base(dir))
		}
	})

	t.Run("not a repo", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := git.Root(dir); err == nil {
			t.Errorf("Root(%q) expected error, got nil", dir)
		}
	})
}

func TestWorktreePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-style absolute paths (/repo); path format is OS-dependent")
	}
	cases := []struct {
		gitRoot string
		branch  string
		want    string
	}{
		{"/repo", "feature/my-feat", "/repo/.worktrees/feature-my-feat"},
		{"/repo", "simple", "/repo/.worktrees/simple"},
		{"/repo", "fix\\stuff", "/repo/.worktrees/fix-stuff"},
		{"/repo", "a b c", "/repo/.worktrees/a-b-c"},
	}
	for _, tc := range cases {
		got := git.WorktreePath(tc.gitRoot, tc.branch)
		if got != tc.want {
			t.Errorf("WorktreePath(%q, %q) = %q, want %q", tc.gitRoot, tc.branch, got, tc.want)
		}
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repoDir := initRepo(t)

	t.Run("create new branch", func(t *testing.T) {
		branch := "test-new-branch"
		wtPath := git.WorktreePath(repoDir, branch)

		if err := git.CreateWorktree(repoDir, branch, wtPath); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("worktree path %q does not exist after creation", wtPath)
		}

		// Verify it shows up in worktree list.
		cmd := exec.Command("git", "-C", repoDir, "worktree", "list")
		out, _ := cmd.Output()
		// git outputs forward slashes; also resolve any OS short-names (Windows 8.3 paths).
		wtPathNorm := wtPath
		if resolved, err := filepath.EvalSymlinks(wtPath); err == nil {
			wtPathNorm = resolved
		}
		if !strings.Contains(filepath.ToSlash(string(out)), filepath.ToSlash(wtPathNorm)) {
			t.Errorf("worktree %q not in `git worktree list` output:\n%s", wtPath, out)
		}
	})

	t.Run("create existing branch falls back to checkout", func(t *testing.T) {
		// Create the branch first via a previous worktree, remove it, then try to create session on it.
		branch := "existing-branch"
		tmpPath := filepath.Join(t.TempDir(), "tmp-wt")
		if err := git.CreateWorktree(repoDir, branch, tmpPath); err != nil {
			t.Fatalf("initial CreateWorktree: %v", err)
		}
		// Remove the worktree so the branch is free but exists.
		if err := git.RemoveWorktree(repoDir, tmpPath); err != nil {
			t.Fatalf("RemoveWorktree: %v", err)
		}

		// Now create again — should fall back to checking out existing branch.
		wtPath := git.WorktreePath(repoDir, branch)
		if err := git.CreateWorktree(repoDir, branch, wtPath); err != nil {
			t.Fatalf("CreateWorktree (existing branch): %v", err)
		}
		if _, err := os.Stat(wtPath); err != nil {
			t.Errorf("worktree path %q does not exist after fallback checkout", wtPath)
		}
		// Cleanup.
		_ = git.RemoveWorktree(repoDir, wtPath)
	})
}

func TestRemoveWorktree(t *testing.T) {
	repoDir := initRepo(t)
	branch := "to-remove"
	wtPath := git.WorktreePath(repoDir, branch)

	if err := git.CreateWorktree(repoDir, branch, wtPath); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if err := git.RemoveWorktree(repoDir, wtPath); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree path %q still exists after removal", wtPath)
	}
}

func TestIsInGitignore(t *testing.T) {
	dir := t.TempDir()

	t.Run("file does not exist", func(t *testing.T) {
		if git.IsInGitignore(dir, ".worktrees") {
			t.Error("IsInGitignore = true, want false (no .gitignore)")
		}
	})

	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte(".worktrees\n.DS_Store\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("pattern present", func(t *testing.T) {
		if !git.IsInGitignore(dir, ".worktrees") {
			t.Error("IsInGitignore = false, want true")
		}
	})

	t.Run("pattern absent", func(t *testing.T) {
		if git.IsInGitignore(dir, "node_modules") {
			t.Error("IsInGitignore = true, want false")
		}
	})
}

func TestAddToGitignore(t *testing.T) {
	t.Run("creates file if missing", func(t *testing.T) {
		dir := t.TempDir()
		if err := git.AddToGitignore(dir, ".worktrees"); err != nil {
			t.Fatalf("AddToGitignore: %v", err)
		}
		if !git.IsInGitignore(dir, ".worktrees") {
			t.Error("pattern not found in newly created .gitignore")
		}
	})

	t.Run("appends to existing file", func(t *testing.T) {
		dir := t.TempDir()
		gi := filepath.Join(dir, ".gitignore")
		if err := os.WriteFile(gi, []byte("*.log\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := git.AddToGitignore(dir, ".worktrees"); err != nil {
			t.Fatalf("AddToGitignore: %v", err)
		}
		if !git.IsInGitignore(dir, ".worktrees") {
			t.Error("pattern not found after appending to .gitignore")
		}
		// Original content must still be there.
		content, _ := os.ReadFile(gi)
		if !strings.Contains(string(content), "*.log") {
			t.Error("original content '*.log' was lost from .gitignore")
		}
	})
}

func TestRandomBranchName(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		name := git.RandomBranchName()
		if !strings.Contains(name, "-") {
			t.Errorf("RandomBranchName() = %q, expected 'adjective-noun' format", name)
		}
		seen[name] = true
	}
	// With 80*80 combinations we expect some variety across 20 draws.
	if len(seen) < 5 {
		t.Errorf("RandomBranchName() produced only %d distinct values in 20 calls; expected more variety", len(seen))
	}
}
