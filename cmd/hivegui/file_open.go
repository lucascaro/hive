// Opening a file the user ⌘-clicked in a session.
//
// The frontend detects path-shaped text and asks ResolveFilePaths
// which candidates exist (that is what decides the hover underline).
// On the click it calls OpenFile. Both re-resolve the path here: the
// frontend's answer is a hint for drawing an underline, never an
// authorisation, because the text it parsed came out of a terminal
// and a compromised renderer could ask for anything at all.
//
// The guard that decides open-vs-reveal is isLaunchable
// (file_open_guard.go). The per-GOOS side effects are openDefault /
// reveal / statMeta in file_open_{darwin,linux,windows,other}.go.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Indirections so tests can assert what would have been launched
// without launching it. Same pattern as runArgv / runGitFn.
var (
	openDefaultFn = openDefault
	revealFn      = reveal
	statMetaFn    = statMeta
	guardPathFn   = guardPath
	runEditorFn   = runEditor
	gooseFn       = func() string { return runtime.GOOS }
)

// maxResolveCandidates caps one ResolveFilePaths call. The frontend
// sends the path-shaped tokens of a single hovered line; a line with
// more candidates than this is noise, not a file list, and each one
// costs a stat.
const maxResolveCandidates = 64

// ResolveFilePaths reports, for each candidate, the absolute path it
// resolves to, or "" when it does not name an existing file. baseDir
// is the session's working directory: the frontend passes the
// worktree path, falling back to the project cwd.
//
// It is deliberately batched — the link provider asks once per hovered
// line rather than once per token.
func (a *App) ResolveFilePaths(baseDir string, candidates []string) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		if i >= maxResolveCandidates {
			break
		}
		abs, err := resolvePath(baseDir, c)
		if err != nil {
			continue
		}
		out[i] = abs
	}
	return out
}

// OpenFile acts on a click on a file path.
//
// editor selects ⇧⌘-click (open in the configured editor) over
// ⌘-click (the OS default app). line and col are 0 when the clicked
// text carried no :line:col suffix.
//
// The three outcomes are: hand the file to the editor, hand it to the
// OS, or reveal it in the file manager without opening it. A
// directory, and anything isLaunchable flags, is revealed.
func (a *App) OpenFile(baseDir, path string, line, col int, editor bool) error {
	abs, err := resolvePath(baseDir, path)
	if err != nil {
		return err
	}
	// Resolve symlinks before the guard: a symlink named notes.md
	// pointing at a .command file is the .command file. A broken
	// symlink keeps the unresolved path and is handled below.
	target := abs
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		target = resolved
	}
	goos := gooseFn()
	meta, err := statMetaFn(target)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(abs), err)
	}

	if editor {
		switch err := runEditorFn(target, line, col, meta.isDir); {
		case err == nil:
			return nil
		case errors.Is(err, errNoEditorConfigured):
			// Fall through to the OS default. Spec criterion 5.
		default:
			return err
		}
	}

	// The name the OS would act on is checked as well as the resolved
	// target: on Windows the handler fires on the name passed to
	// ShellExecute, and `evil.exe.` must not slip through because its
	// symlink target reads clean.
	if meta.isDir || isLaunchable(goos, guardPathFn(target), meta) || isLaunchable(goos, guardPathFn(abs), meta) {
		return revealFn(target)
	}
	return openDefaultFn(target)
}

// isNetworkOrDevicePath reports whether p names something other than a
// file on this machine's own disks.
//
// This is the one guard that has to fire before the stat, not before
// the open: resolvePath is called from ResolveFilePaths, which runs on
// *hover* to decide the underline. On Windows a UNC path like
// `\\evil.example.com\share\a.txt` makes Lstat dial that host and
// authenticate, handing over the user's NTLM hash — so a session that
// merely prints such a path would leak credentials with no click at
// all. The path came out of a terminal, so that text is not ours to
// trust.
//
// Checked on every platform rather than under a GOOS test: the rule is
// about what the *name* means, the test is free, and a guard that only
// exists on the platform it was written for is the hole this feature
// has already grown once.
func isNetworkOrDevicePath(p string) bool {
	slashed := strings.ReplaceAll(p, `\`, "/")
	// //host/share (UNC), //?/... and //./... (Win32 device namespace,
	// which reaches \\.\pipe\ and friends).
	return strings.HasPrefix(slashed, "//")
}

// resolvePath turns a candidate from terminal text into an absolute
// path that exists, or an error. It expands a leading ~, resolves a
// relative path against baseDir, and cleans the result — so no
// argument built from it can start with a '-' and be read as a flag.
//
// It does not resolve symlinks; callers that care do that themselves.
func resolvePath(baseDir, path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", errors.New("empty path")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p[1:], string(filepath.Separator)))
	}
	if isNetworkOrDevicePath(p) {
		return "", fmt.Errorf("refusing network or device path: %s", p)
	}
	if !filepath.IsAbs(p) {
		if baseDir == "" {
			return "", fmt.Errorf("relative path %q with no base directory", path)
		}
		p = filepath.Join(baseDir, p)
	}
	p = filepath.Clean(p)
	// Re-check after the join. On Windows a UNC baseDir survives Join and
	// Clean, so a plain relative candidate could still land on the
	// network; on unix Join collapses the leading slashes and this is a
	// no-op. The candidate itself was already checked above — that is the
	// attacker-controlled half, since baseDir is the session's own
	// worktree or project directory.
	if isNetworkOrDevicePath(p) {
		return "", fmt.Errorf("refusing network or device path: %s", p)
	}
	if _, err := os.Lstat(p); err != nil {
		return "", fmt.Errorf("no such file: %s", p)
	}
	return p, nil
}
