// Package git provides utilities for detecting and interacting with git repositories,
// including worktree lifecycle management for hive sessions.
package git

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsGitRepo reports whether dir (or any of its parents) is inside a git repository.
func IsGitRepo(dir string) bool {
	_, err := Root(dir)
	return err == nil
}

// Root returns the absolute path of the git repository root that contains dir.
func Root(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WorktreePath returns the path where a worktree for the given branch should be created.
// Worktrees are placed at <gitRoot>/.worktrees/<sanitizedBranch>.
func WorktreePath(gitRoot, branch string) string {
	return filepath.Join(gitRoot, ".worktrees", sanitizeBranch(branch))
}

// CreateWorktree creates a git worktree at worktreePath for the given branch.
// It first attempts to create a new branch from HEAD (-b). If a branch with that name
// already exists, it falls back to checking out the existing branch.
func CreateWorktree(repoDir, branch, worktreePath string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Try creating a new branch first.
	cmd := exec.Command("git", "-C", repoDir, "worktree", "add", "-b", branch, worktreePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		// If the branch already exists, fall back to checking it out.
		if strings.Contains(string(out), "already exists") || strings.Contains(string(out), "fatal: A branch named") {
			cmd2 := exec.Command("git", "-C", repoDir, "worktree", "add", worktreePath, branch)
			if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
				return fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out2)))
			}
			return nil
		}
		return fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func RemoveWorktree(repoDir, worktreePath string) error {
	// Use --force to remove even if the worktree has modified files.
	cmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", worktreePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}
	// Best-effort removal of the directory if it still exists.
	_ = os.RemoveAll(worktreePath)
	return nil
}

// IsInGitignore reports whether pattern already appears (as its own line) in the
// .gitignore file at gitRoot. Returns false if the file does not exist.
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
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n# hive worktrees\n%s\n", pattern)
	return err
}

// RandomBranchName returns a random "adjective-noun" branch name suggestion.
func RandomBranchName() string {
	adj := adjectives[rand.Intn(len(adjectives))]
	noun := nouns[rand.Intn(len(nouns))]
	return adj + "-" + noun
}

// sanitizeBranch replaces characters that are invalid in directory names.
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
