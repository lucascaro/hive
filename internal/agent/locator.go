package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Locator finds the agent's most recent on-disk conversation ID for
// the given cwd, considering only files modified at or after `since`.
//
// Returns ("", nil) when nothing matches — that is the normal "no
// conversation found yet" case and callers should not treat it as an
// error. Returns an error only for unexpected I/O failures.
//
// `agentDataRoot` is the agent's top-level data directory
// (e.g. "~/.claude" or "~/.codex"); production callers resolve it via
// os.UserHomeDir(); tests pass a temp dir.
type Locator func(agentDataRoot, cwd string, since time.Time) (string, error)

// LocatorFor returns the locator for the given agent ID, or nil if
// the agent does not expose a stable on-disk conversation ID.
func LocatorFor(id ID) Locator {
	switch id {
	case IDClaude:
		return claudeLocator
	case IDCodex:
		return codexLocator
	default:
		return nil
	}
}

// ClaudeProjectsRoot returns the production data root for Claude
// (~/.claude). Used by callers that don't want to hard-code the path.
func ClaudeProjectsRoot() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".claude")
	}
	return ""
}

// CodexSessionsRoot returns the production data root for Codex
// (~/.codex).
func CodexSessionsRoot() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".codex")
	}
	return ""
}

// claudeLocator scans <agentDataRoot>/projects/<encoded-cwd>/*.jsonl
// and returns the conversation ID (filename without ".jsonl") of the
// newest file modified at or after `since`. Claude encodes paths by
// replacing every "/" with "-".
func claudeLocator(agentDataRoot, cwd string, since time.Time) (string, error) {
	if agentDataRoot == "" || cwd == "" {
		return "", nil
	}
	encoded := encodeClaudeCwd(cwd)
	dir := filepath.Join(agentDataRoot, "projects", encoded)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil // layout missing; graceful degrade
		}
		return "", err
	}
	var (
		bestID   string
		bestTime time.Time
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime()
		if mt.Before(since) {
			continue
		}
		if mt.After(bestTime) {
			bestTime = mt
			bestID = strings.TrimSuffix(e.Name(), ".jsonl")
		}
	}
	return bestID, nil
}

// encodeClaudeCwd mirrors Claude Code's path-encoding: every os
// separator (and a leading separator) becomes "-". E.g.
// "/Users/x/checkout/y" → "-Users-x-checkout-y".
func encodeClaudeCwd(cwd string) string {
	return strings.ReplaceAll(cwd, string(filepath.Separator), "-")
}

// codexLocator walks <agentDataRoot>/sessions/YYYY/MM/DD/ for
// rollout files, returning the trailing UUID of the newest file
// modified at or after `since`. Codex filenames look like:
//
//	rollout-2026-04-02T07-28-09-019d4e98-0b7d-7751-89ca-8a4386362f54.jsonl
//
// The UUID is the last 5 hyphen-separated groups (8-4-4-4-12) before
// ".jsonl". We're tolerant of layout drift: if we cannot parse a UUID
// from a filename, we skip it.
//
// The walk depth is bounded (year/month/day/file) so a malicious or
// misconfigured symlink loop cannot hang us.
func codexLocator(agentDataRoot, cwd string, since time.Time) (string, error) {
	// Codex sessions are not partitioned by cwd on disk, so we can
	// only return the most-recent-globally rollout. The cwd parameter
	// is accepted for interface symmetry but is unused.
	_ = cwd
	if agentDataRoot == "" {
		return "", nil
	}
	root := filepath.Join(agentDataRoot, "sessions")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var (
		bestID   string
		bestTime time.Time
	)
	// Walk with explicit depth limit (root=0, year=1, month=2, day=3, file=4)
	const maxDepth = 4
	rootDepth := strings.Count(root, string(filepath.Separator))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		depth := strings.Count(path, string(filepath.Separator)) - rootDepth
		if depth > maxDepth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mt := info.ModTime()
		if mt.Before(since) {
			return nil
		}
		id := parseCodexUUID(d.Name())
		if id == "" {
			return nil
		}
		if mt.After(bestTime) {
			bestTime = mt
			bestID = id
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return bestID, nil
}

// parseCodexUUID extracts the trailing 8-4-4-4-12 UUID from a Codex
// rollout filename like "rollout-2026-04-02T07-28-09-019d4e98-0b7d-7751-89ca-8a4386362f54.jsonl".
// Returns "" if no valid UUID is found.
func parseCodexUUID(name string) string {
	name = strings.TrimSuffix(name, ".jsonl")
	parts := strings.Split(name, "-")
	if len(parts) < 5 {
		return ""
	}
	tail := parts[len(parts)-5:]
	if len(tail[0]) != 8 || len(tail[1]) != 4 || len(tail[2]) != 4 || len(tail[3]) != 4 || len(tail[4]) != 12 {
		return ""
	}
	for _, p := range tail {
		for _, r := range p {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return ""
			}
		}
	}
	return strings.Join(tail, "-")
}
