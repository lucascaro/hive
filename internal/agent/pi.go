package agent

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// piExtensionSource is the Hive reporter extension Pi sessions load.
// Embedded rather than shipped as a separate file so a Hive binary is
// self-contained: there is no install step, and the extension a daemon
// writes always matches the daemon that wrote it.
//
//go:embed pi/hive.ts
var piExtensionSource string

// PiExtensionRelPath is where EnsurePiExtension writes the extension,
// relative to the state dir.
var PiExtensionRelPath = filepath.Join("pi", "hive.ts")

// EnsurePiExtension writes the embedded Pi extension to
// <stateDir>/pi/hive.ts, atomically (temp + rename) and only when the
// content differs, so an upgraded daemon replaces a stale copy but a
// restart of the same build touches nothing.
//
// The state dir is the registry's, and the registry is normally its
// only writer (DESIGN.md); this is a named exception, like the GUI's
// agents.json — it is not session state, never crosses the wire, and
// follows the same temp + rename discipline.
//
// A failure is logged and returned but must never stop the daemon: the
// Pi adapter's os.Stat check turns a missing extension into "Pi runs on
// the heuristic tier" rather than into a broken spawn.
func EnsurePiExtension(stateDir string) error {
	if stateDir == "" {
		return nil
	}
	dst := filepath.Join(stateDir, PiExtensionRelPath)
	if cur, err := os.ReadFile(dst); err == nil && string(cur) == piExtensionSource {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".hive-*.ts")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeded
	if _, err := tmp.WriteString(piExtensionSource); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

var piExtensionWarnOnce sync.Once

// piSpawnArgs is Def.SpawnArgs for Pi: `-e <stateDir>/pi/hive.ts` when
// that file is on disk, nil otherwise. The file being absent means
// EnsurePiExtension failed (or was never called, as in a test binary),
// and passing -e for a path Pi cannot read fails the whole spawn — so
// the miss degrades to the heuristic tier instead.
func piSpawnArgs(sp SpawnInfo) []string {
	if sp.StateDir == "" {
		return nil
	}
	path := filepath.Join(sp.StateDir, PiExtensionRelPath)
	if _, err := os.Stat(path); err != nil {
		piExtensionWarnOnce.Do(func() {
			log.Printf("agent: pi extension not found at %s (%v); pi sessions run on the heuristic state tier only", path, err)
		})
		return nil
	}
	return []string{"-e", path}
}

// encodePiSessionsDir mirrors pi's on-disk encoding for the per-cwd
// transcript directory under ~/.pi/agent/sessions/: the leading "/" is
// dropped, the remaining separators become "-", and the whole thing is
// wrapped in a literal "--" at both ends.
//
// Deliberately NOT encodeClaudeProjectDir. Claude folds "." to "-" as
// well; pi does not, so ".worktrees" stays ".worktrees". Reusing the
// claude encoder here resolves to a directory that does not exist, and
// the failure is silent — the session just looks like it has no
// history. TestEncodePiSessionsDir pins the difference.
func encodePiSessionsDir(cwd string) string {
	s := filepath.ToSlash(filepath.Clean(cwd))
	s = strings.TrimPrefix(s, "/")
	s = strings.ReplaceAll(s, "/", "-")
	return "--" + s + "--"
}

// piTranscriptPaths returns the transcript files pi wrote for
// sessionID under cwd, oldest first, or nil when none exist.
//
// pi names each file "<timestamp>_<session-id>.jsonl" using the
// --session-id it was given verbatim — Hive passes its own entry id
// there (registry.appendSpawnArgs), so the suffix is an exact handle
// rather than a heuristic. The timestamp prefix is pi's own and not
// derivable, hence a glob rather than a join.
//
// A slice rather than a single path: nothing guarantees pi writes only
// one file per id, and sorting by name puts them in timestamp order
// for free. Observed reality today is one file per id.
func piTranscriptPaths(sessionID, cwd string) []string {
	if sessionID == "" || cwd == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".pi", "agent", "sessions", encodePiSessionsDir(cwd))
	// The id goes through filepath.Match as a literal, so an id
	// containing a glob metacharacter would match the wrong files.
	// Hive ids are uuids, but probe/test ids are arbitrary strings.
	if strings.ContainsAny(sessionID, "*?[\\") {
		return nil
	}
	hits, err := filepath.Glob(filepath.Join(dir, "*_"+sessionID+".jsonl"))
	if err != nil || len(hits) == 0 {
		return nil
	}
	sort.Strings(hits)
	return hits
}
