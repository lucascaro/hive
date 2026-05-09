package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// claudeSessionExists reports whether claude has persisted a transcript
// for sessionID under cwd. Claude only writes the JSONL after the first
// user message, so a session started but never used has no on-disk
// record and `claude --resume <id>` exits with "No conversation found
// with session ID". The Hive Restart flow has to detect that and re-pin
// the same id with --session-id instead.
//
// Layout: ~/.claude/projects/<encoded-cwd>/<id>.jsonl, where the
// encoding replaces every "/" with "-".
var claudeSessionExists = func(sessionID, cwd string) bool {
	if sessionID == "" || cwd == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	encoded := strings.ReplaceAll(cwd, "/", "-")
	_, err = os.Stat(filepath.Join(home, ".claude", "projects", encoded, sessionID+".jsonl"))
	return err == nil
}

// SetClaudeSessionExistsForTest replaces the on-disk transcript probe
// with a stub. Returns a restore function to defer in tests. Lives in
// a regular .go file (not _test.go) so it's reachable from other
// packages' tests, e.g. registry_test.go.
func SetClaudeSessionExistsForTest(fn func(sessionID, cwd string) bool) (restore func()) {
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
