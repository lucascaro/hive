package agent

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// copilotSessionStateDir is the directory copilot writes its
// per-session state under. Tests swap this to a t.TempDir() to avoid
// touching the real filesystem. Default resolves to
// "$HOME/.copilot/session-state".
var copilotSessionStateDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".copilot", "session-state")
}()

// copilotUUIDPattern matches the canonical 8-4-4-4-12 hex layout that
// copilot uses for its session-state directory names.
var copilotUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

// copilotCapturePollInterval mirrors codex's cadence: 200ms keeps
// capture latency low while not thrashing the disk.
var copilotCapturePollInterval = 200 * time.Millisecond

// copilotCaptureSessionID polls copilot's session-state directory for
// a `<UUID>/` subdirectory whose `workspace.yaml` records the spawn
// cwd and whose mtime is at or after spawnedAt-1s. Returns the UUID
// (= directory name).
//
// Strategy mirrors codexCaptureSessionID: poll every
// copilotCapturePollInterval until ctx is done. Per poll, list
// session-state subdirectories, skip names that aren't valid UUIDs,
// skip dirs whose workspace.yaml mtime is before the cutoff, skip
// dirs we've already inspected and rejected (negative cache), read
// workspace.yaml and confirm the `cwd:` field. First cwd-match wins.
//
// We do not snapshot "dirs seen on the first poll" as preexisting
// (same reasoning as codex): copilot creates the session-state dir
// very early in the run, often before our first poll tick. The
// mtime-cutoff plus cwd-match disambiguates: a prior copilot run in
// the same cwd has an mtime well before spawnedAt-1s.
func copilotCaptureSessionID(ctx context.Context, cwd string, spawnedAt time.Time) (string, error) {
	if copilotSessionStateDir == "" {
		return "", errors.New("copilot session-state dir unresolved (no HOME)")
	}
	cutoff := spawnedAt.Add(-time.Second)
	rejected := map[string]struct{}{}

	ticker := time.NewTicker(copilotCapturePollInterval)
	defer ticker.Stop()

	for {
		for _, m := range scanCopilotSessionDirs(copilotSessionStateDir, cutoff) {
			if _, seen := rejected[m.path]; seen {
				continue
			}
			if id, ok := readCopilotWorkspaceCwd(m.path, m.uuid, cwd); ok {
				return id, nil
			}
			rejected[m.path] = struct{}{}
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

type copilotSessionMatch struct {
	path    string // <session-state>/<UUID>
	uuid    string // directory basename, validated as UUID
	modTime time.Time
}

// scanCopilotSessionDirs lists immediate subdirectories of root and
// returns every <UUID> dir whose workspace.yaml mtime is at or after
// cutoff. Missing workspace.yaml is treated as "not yet ready" and
// skipped (no error).
func scanCopilotSessionDirs(root string, cutoff time.Time) []copilotSessionMatch {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []copilotSessionMatch
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		if !copilotUUIDPattern.MatchString(name) {
			continue
		}
		dirPath := filepath.Join(root, name)
		wsPath := filepath.Join(dirPath, "workspace.yaml")
		info, ierr := os.Stat(wsPath)
		if ierr != nil {
			continue // workspace.yaml not written yet
		}
		if info.ModTime().Before(cutoff) {
			continue
		}
		out = append(out, copilotSessionMatch{path: dirPath, uuid: name, modTime: info.ModTime()})
	}
	return out
}

// readCopilotWorkspaceCwd reads <path>/workspace.yaml and returns
// (uuid, true) when its `cwd:` field matches wantCwd. The uuid is
// passed in by the scanner (= directory basename, already validated).
//
// We parse manually instead of pulling in a YAML dependency: the
// file is small (always the same handful of top-level keys) and we
// only care about one field.
func readCopilotWorkspaceCwd(dirPath, uuid, wantCwd string) (string, bool) {
	wsPath := filepath.Join(dirPath, "workspace.yaml")
	f, err := os.Open(wsPath)
	if err != nil {
		return "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Cap line length so a malformed/binary file can't spike memory.
	scanner.Buffer(make([]byte, 0, 8*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Top-level keys only — copilot's workspace.yaml has cwd at
		// the document root. Indented lines are nested objects and
		// not what we want.
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		const key = "cwd:"
		if !strings.HasPrefix(line, key) {
			continue
		}
		val := strings.TrimSpace(line[len(key):])
		// Strip quotes (YAML allows both single and double quoted
		// strings; copilot writes unquoted, but be defensive).
		val = strings.Trim(val, `"'`)
		if val == wantCwd {
			return uuid, true
		}
		return "", false
	}
	return "", false
}
