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

// codexRolloutPattern matches "rollout-<ISO-timestamp>-<UUID>.jsonl".
// The UUID format is the canonical 8-4-4-4-12 hex layout codex uses.
// Used as a filename filter only — the session ID comes from the
// file's session_meta record, not from the name.
var codexRolloutPattern = regexp.MustCompile(
	`^rollout-.+-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`,
)

// codexCapturePollInterval is how often the capture goroutine scans
// the sessions directory for a matching rollout file. Codex writes
// the file very early in the run; 200ms keeps the capture latency
// low without thrashing the disk.
var codexCapturePollInterval = 200 * time.Millisecond

// codexCaptureSessionID polls codex's session-rollout directory for a
// rollout file whose mtime is >= spawnedAt-1s and whose first line
// records `payload.cwd == cwd`. Returns that record's `payload.id`.
//
// Strategy: poll every codexCapturePollInterval until ctx is done.
// Per poll, walk the directory, ignore files whose name doesn't
// match the rollout pattern or whose mtime is older than
// spawnedAt-1s (clock fuzz), skip files we've already inspected and
// definitively rejected, and read just the first line of each
// remaining candidate to confirm the cwd. First cwd-match wins.
// A file caught mid-write is "not ready", not rejected — see
// cwdResult.
//
// We deliberately do NOT snapshot "files seen on the first poll" as
// pre-existing: codex frequently creates its rollout within
// milliseconds of fork, before our first poll tick fires. A
// snapshot-and-skip would classify our own rollout as pre-existing
// and lose it forever. Instead, mtime + cwd check together
// disambiguate: a prior codex run that happened to share this cwd
// will have an mtime well before spawnedAt-1s (different process,
// different invocation, different time) and is filtered by the
// cutoff. Two truly concurrent codex spawns in the same cwd within
// the same second are unsupported (no reliable signal to
// disambiguate); the first cwd-match wins.
func codexCaptureSessionID(ctx context.Context, cwd string, spawnedAt time.Time) (string, error) {
	if codexSessionsDir == "" {
		return "", errors.New("codex sessions dir unresolved (no HOME)")
	}
	// Slack on the spawn time: filesystem mtime resolution and the
	// gap between our `time.Now()` and codex's first write are both
	// imprecise. A small backstep prevents skipping a file written
	// in the same second we recorded.
	cutoff := spawnedAt.Add(-time.Second)
	return pollCaptureSessionID(ctx, codexSessionsDir, cutoff, codexCapturePollInterval,
		scanCodexRollouts,
		func(path string) (string, cwdResult) { return readCodexRolloutCwd(path, cwd) },
	)
}

// scanCodexRollouts walks the sessions tree and returns the path of
// every rollout file whose mtime is at or after cutoff. The directory
// layout is sessions/YYYY/MM/DD/rollout-*.jsonl; we walk the whole
// tree (cheap — only a few hundred files even on heavy users) so we
// don't miss the day-boundary case where the spawn straddles midnight.
func scanCodexRollouts(root string, cutoff time.Time) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !codexRolloutPattern.MatchString(d.Name()) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out
}

// readCodexRolloutCwd reads the first line of a rollout file and
// returns (payload.id, cwdMatch) when payload.cwd matches the given
// cwd. Keeping JSON parsing scoped to the first record keeps this
// fast.
//
// Returns cwdNotReady for I/O errors, a first line with no terminating
// newline (codex is still writing it — a real rollout always has
// records after session_meta), or JSON that doesn't parse. Callers
// must NOT negative-cache these: the file may still be partially
// written, and rejecting it for good loses the capture.
func readCodexRolloutCwd(path, wantCwd string) (string, cwdResult) {
	f, err := os.Open(path)
	if err != nil {
		return "", cwdNotReady
	}
	defer f.Close()
	// First line of a rollout is the session_meta record. Cap the
	// read so a runaway/binary file can't spike memory.
	br := bufio.NewReaderSize(f, 64*1024)
	line, err := br.ReadString('\n')
	if err != nil {
		return "", cwdNotReady
	}
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if jerr := json.Unmarshal([]byte(strings.TrimRight(line, "\n")), &rec); jerr != nil {
		return "", cwdNotReady
	}
	if rec.Type != "session_meta" || rec.Payload.Cwd != wantCwd {
		return "", cwdMismatch
	}
	return rec.Payload.ID, cwdMatch
}
