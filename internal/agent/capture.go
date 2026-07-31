package agent

import (
	"context"
	"time"
)

// cwdResult is the verdict a session-capture check returns for one
// candidate. The distinction that matters is cwdNotReady vs
// cwdMismatch: only a definitive mismatch may be negative-cached. A
// file the agent is still writing must stay eligible for the next
// poll, or the capture is lost for good.
type cwdResult int

const (
	cwdNotReady cwdResult = iota // I/O error, or the file isn't fully written yet
	cwdMatch                     // definitively ours
	cwdMismatch                  // definitively not ours
)

// pollCaptureSessionID polls for the session file an agent writes on
// spawn, and returns the session ID once one is confirmed to belong to
// our cwd.
//
// scan lists candidate keys (paths) whose mtime is at or after cutoff;
// check reads one candidate and reports (id, verdict). check closes
// over the caller's cwd. Candidates that come back cwdMismatch are
// negative-cached so they aren't re-read every tick; cwdNotReady ones
// are deliberately left uncached.
//
// Returns ctx.Err() if the context ends before a match — capture is
// best-effort and the caller treats a miss as "no session ID".
//
// The per-agent scanners deliberately stay separate: codex walks a
// YYYY/MM/DD tree matching a filename regex and reads the ID out of a
// JSON record, copilot lists one level of UUID-named dirs and reads a
// YAML field, with the ID coming from the dir name. Only this loop —
// ticker, negative cache, ctx handling — is genuinely common.
func pollCaptureSessionID(
	ctx context.Context,
	root string,
	cutoff time.Time,
	interval time.Duration,
	scan func(root string, cutoff time.Time) []string,
	check func(key string) (string, cwdResult),
) (string, error) {
	rejected := map[string]struct{}{}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		for _, key := range scan(root, cutoff) {
			if _, seen := rejected[key]; seen {
				continue
			}
			switch id, res := check(key); res {
			case cwdMatch:
				return id, nil
			case cwdMismatch:
				rejected[key] = struct{}{}
			case cwdNotReady:
			}
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
