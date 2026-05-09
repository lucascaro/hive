package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// codexSessionsDir is the directory codex writes its rollout files
// under. Tests swap this to a t.TempDir() to avoid touching the real
// filesystem. Default resolves to "$HOME/.codex/sessions".
var codexSessionsDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}()

// codexRolloutPattern matches "rollout-<ISO-timestamp>-<UUID>.jsonl"
// with the UUID captured. The UUID format is the canonical
// 8-4-4-4-12 hex layout codex uses.
var codexRolloutPattern = regexp.MustCompile(
	`^rollout-.+-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`,
)

// codexCapturePollInterval is how often the capture goroutine scans
// the sessions directory for a matching rollout file. Codex writes
// the file very early in the run; 200ms keeps the capture latency
// low without thrashing the disk.
var codexCapturePollInterval = 200 * time.Millisecond

// codexCaptureSessionID polls codex's session-rollout directory for a
// new file whose first line records `payload.cwd == cwd` and whose
// mtime is >= spawnedAt. Returns the UUID parsed from the filename.
//
// Strategy: poll every codexCapturePollInterval until ctx is done.
// Per poll, walk the directory, ignore files whose name doesn't match
// the rollout pattern, ignore files older than spawnedAt-1s (clock
// fuzz), and read just the first line of each candidate to confirm
// the cwd. First match wins. Files seen on the first poll are
// recorded and re-skipped on subsequent polls so two near-simultaneous
// codex spawns in the same cwd don't latch onto each other's file.
func codexCaptureSessionID(ctx context.Context, cwd string, spawnedAt time.Time) (string, error) {
	if codexSessionsDir == "" {
		return "", errors.New("codex sessions dir unresolved (no HOME)")
	}
	// Slack on the spawn time: filesystem mtime resolution and the
	// gap between our `time.Now()` and codex's first write are both
	// imprecise. A small backstep prevents skipping a file written
	// in the same second we recorded.
	cutoff := spawnedAt.Add(-time.Second)
	preexisting := map[string]struct{}{}
	first := true

	ticker := time.NewTicker(codexCapturePollInterval)
	defer ticker.Stop()

	for {
		matches := scanCodexRollouts(codexSessionsDir, cutoff)
		for _, m := range matches {
			if first {
				preexisting[m.path] = struct{}{}
				continue
			}
			if _, seen := preexisting[m.path]; seen {
				continue
			}
			if id, ok := readCodexRolloutCwd(m.path, cwd); ok {
				return id, nil
			}
			// Negative result: remember so we don't re-read it.
			preexisting[m.path] = struct{}{}
		}
		first = false

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

type codexRolloutMatch struct {
	path string
	uuid string
}

// scanCodexRollouts walks the sessions tree and returns every rollout
// file whose mtime is at or after cutoff. The directory layout is
// sessions/YYYY/MM/DD/rollout-*.jsonl; we walk the whole tree
// (cheap — only a few hundred files even on heavy users) so we don't
// miss the day-boundary case where the spawn straddles midnight.
func scanCodexRollouts(root string, cutoff time.Time) []codexRolloutMatch {
	var out []codexRolloutMatch
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		m := codexRolloutPattern.FindStringSubmatch(name)
		if m == nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		out = append(out, codexRolloutMatch{path: path, uuid: m[1]})
		return nil
	})
	return out
}

// readCodexRolloutCwd reads the first line of a rollout file and
// returns (uuid, true) when payload.cwd matches the given cwd. The
// uuid is taken from the filename (already validated by the regex
// when the file was scanned) — keeping JSON parsing scoped to the
// cwd check keeps this fast.
func readCodexRolloutCwd(path, wantCwd string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	// First line of a rollout is the session_meta record. Cap the
	// read so a runaway/binary file can't spike memory.
	br := bufio.NewReaderSize(f, 64*1024)
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if jerr := json.Unmarshal([]byte(strings.TrimRight(line, "\n")), &rec); jerr != nil {
		return "", false
	}
	if rec.Type != "session_meta" || rec.Payload.Cwd != wantCwd {
		return "", false
	}
	return rec.Payload.ID, true
}
