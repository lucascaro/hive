package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// encodeClaudeProjectDir mirrors claude's on-disk encoding for the
// per-cwd transcript directory under ~/.claude/projects/. Claude
// replaces both path separators and the "." in dotted segments (e.g.
// .worktrees) with "-", so /Users/u/repo/.worktrees/x becomes
// "-Users-u-repo--worktrees-x". On Windows we normalize backslashes
// to forward slashes first and replace the drive colon so the probe
// has a chance of matching whatever path-flavor claude itself wrote.
func encodeClaudeProjectDir(cwd string) string {
	s := filepath.ToSlash(filepath.Clean(cwd))
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, ":", "-")
	return s
}

// claudeSessionExists reports whether claude has persisted a transcript
// for sessionID under cwd. Claude only writes the JSONL after the first
// user message, so a session started but never used has no on-disk
// record and `claude --resume <id>` exits with "No conversation found
// with session ID". The Hive Restart flow has to detect that and re-pin
// the same id with --session-id instead.
//
// Layout: ~/.claude/projects/<encoded-cwd>/<id>.jsonl. See
// encodeClaudeProjectDir for the encoding.
var claudeSessionExists = func(sessionID, cwd string) bool {
	if sessionID == "" || cwd == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	encoded := encodeClaudeProjectDir(cwd)
	_, err = os.Stat(filepath.Join(home, ".claude", "projects", encoded, sessionID+".jsonl"))
	return err == nil
}

// SetClaudeSessionExistsForTest replaces the on-disk transcript probe
// with a stub. Returns a restore function to defer in tests. Lives in
// a regular .go file (not _test.go) so it's reachable from other
// packages' tests, e.g. registry_test.go.
//
// Callers must not run with t.Parallel() while the override is
// installed: the hook is package-global and concurrent overrides will
// race. fn must be non-nil; passing nil panics rather than deferring
// the failure to the next claudeResumeArgs call.
func SetClaudeSessionExistsForTest(fn func(sessionID, cwd string) bool) (restore func()) {
	if fn == nil {
		panic("agent.SetClaudeSessionExistsForTest: nil fn")
	}
	prev := claudeSessionExists
	claudeSessionExists = fn
	return func() { claudeSessionExists = prev }
}

func claudeResumeArgs(sessionID, cwd string) []string {
	if claudeSessionExists(sessionID, cwd) {
		return []string{"claude", "--resume", sessionID}
	}
	return []string{"claude", "--session-id", sessionID}
}
