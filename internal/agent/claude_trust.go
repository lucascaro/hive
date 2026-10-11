package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ClaudeStatePath is Claude Code's global state file, which records the
// folders whose workspace-trust dialog the user accepted. It also holds
// account data, so ClaudeFolderTrusted decodes only the trust flag.
func ClaudeStatePath(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	return filepath.Join(home, ".claude.json")
}

// ClaudeFolderTrusted reports whether the Claude state file at statePath
// marks cwd, or any ancestor of it, as trusted (projects[path].
// hasTrustDialogAccepted), which is the check Claude's own trust dialog
// makes. It answers false when it cannot tell: no file, unreadable, or
// not JSON. The file's schema is Claude's, not documented, and Claude
// has been inconsistent about ancestors, so a caller treats false as
// "not known to be trusted", never as "known to be untrusted".
func ClaudeFolderTrusted(statePath, cwd string) bool {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	var st struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	trusted := make(map[string]bool, len(st.Projects))
	for k, v := range st.Projects {
		if v.Trusted {
			trusted[trustKey(k)] = true
		}
	}
	for _, start := range trustStarts(cwd) {
		for dir := start; ; {
			if trusted[trustKey(dir)] {
				return true
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return false
}

// trustStarts is cwd as given and with symlinks resolved, because Claude
// records whichever spelling it was launched in (macOS /tmp is
// /private/tmp).
func trustStarts(cwd string) []string {
	cwd = filepath.Clean(cwd)
	out := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		out = append(out, real)
	}
	return out
}

// trustKey normalizes a path for comparison: slashes forward, no
// trailing separator, and case folded where the filesystem usually is.
func trustKey(p string) string {
	p = strings.TrimRight(filepath.ToSlash(filepath.Clean(p)), "/")
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// SamePath reports whether a and b name the same folder, through
// symlinks and, on case-insensitive filesystems, case.
func SamePath(a, b string) bool {
	for _, x := range trustStarts(a) {
		for _, y := range trustStarts(b) {
			if trustKey(x) == trustKey(y) {
				return true
			}
		}
	}
	return false
}
