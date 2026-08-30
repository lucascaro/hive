package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hiveModulePath is the module line every real hive checkout declares.
// Matching on it is what stops the upward walk from happily settling on
// some *other* Go project that happens to enclose the binary — building
// that would produce something that is not hive at all.
const hiveModulePath = "github.com/lucascaro/hive"

// executablePath is a seam so tests can drive the auto-detect branch
// without installing themselves into a fake checkout. Mirrors the
// looksLikeHivedFn/waitForExitFn pattern in restart_unix.go.
var executablePath = os.Executable

// SourceRepoStatus is what the Settings modal renders next to the
// source-repo row: where the checkout was found and how.
type SourceRepoStatus struct {
	Path string `json:"path"`
	// Detected is true when Path came from walking up out of the
	// running binary rather than from the saved override, so the UI can
	// say "detected" instead of implying the user configured it.
	Detected bool   `json:"detected"`
	Error    string `json:"error"`
}

// resolveSourceRepo returns the hive checkout the "latest" channel
// should pull and build.
//
// Order: the configured override first (an explicit choice always wins
// over a guess), then a walk up from the running binary. The walk is
// what makes the channel zero-config for a local `./build.sh` install —
// and it is exactly what cannot work for a bundle in /Applications,
// which is why the override exists at all.
func resolveSourceRepo(configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		abs, err := filepath.Abs(strings.TrimSpace(configured))
		if err != nil {
			return "", fmt.Errorf("source repo %q: %w", configured, err)
		}
		if err := validateSourceRepo(abs); err != nil {
			return "", err
		}
		return abs, nil
	}
	self, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	dir := filepath.Dir(self)
	// EvalSymlinks so a binary reached through a symlinked bin dir still
	// walks up its real directory tree.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	for {
		if validateSourceRepo(dir) == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no hive checkout found above %s — set a source repo in Settings", filepath.Dir(self))
}

// validateSourceRepo reports whether dir is a hive checkout we can
// actually pull and build: a git repo, with build.sh, whose go.mod
// declares the hive module.
func validateSourceRepo(dir string) error {
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || (!st.IsDir() && !st.Mode().IsRegular()) {
		// .git is a directory in a normal clone and a file in a worktree;
		// both are fine, anything else is not a checkout.
		return fmt.Errorf("%s is not a git checkout", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "build.sh")); err != nil {
		return fmt.Errorf("%s has no build.sh", dir)
	}
	mod, err := goModModulePath(filepath.Join(dir, "go.mod"))
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	if mod != hiveModulePath {
		return fmt.Errorf("%s is module %q, not %q", dir, mod, hiveModulePath)
	}
	return nil
}

// goModModulePath reads just the module line. Hand-parsed rather than
// pulled from golang.org/x/mod: one line, no new dependency.
func goModModulePath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	return "", fmt.Errorf("go.mod has no module line")
}

// SourceRepoStatusFor is the Wails binding the Settings modal calls as
// the user edits the path, so the row can say "detected" / "not found"
// without saving first.
func (a *App) SourceRepoStatusFor(configured string) SourceRepoStatus {
	path, err := resolveSourceRepo(configured)
	if err != nil {
		return SourceRepoStatus{Error: err.Error()}
	}
	return SourceRepoStatus{Path: path, Detected: strings.TrimSpace(configured) == ""}
}
