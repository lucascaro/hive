package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/proc"
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

// claudeTranscriptPaths returns the transcript file holding the
// conversation Hive started as sessionID under cwd, or nil when none
// exists.
//
// Usually that is <encoded-cwd>/<sessionID>.jsonl — Claude pins the
// conversation to the id Hive chose (SessionIDFlag). But Claude can FORK a
// conversation into a new file: backgrounding a session as a job,
// --fork-session, /branch, resuming from its picker. The fork copies the
// whole history, gets a new "sessionId" of its own, and carries on there,
// while the original file stops growing. Reading only <sessionID>.jsonl
// then silently cuts the transcript off at the fork.
//
// A fork keeps the original id in its records' snake_case "session_id"
// field (verified on a real fork: 608 records with sessionId=<fork> and
// session_id=<original>). So the newest file in the directory that
// descends from sessionID is the live one. Only files modified after the
// original are candidates — a fork is written after the file it copied —
// which excludes nearly everything in a busy project directory.
//
// A slice for the TranscriptPaths contract, but always at most one path:
// a fork already contains the history, so concatenating the original and
// the fork would show everything twice.
func claudeTranscriptPaths(sessionID, cwd string) []string {
	if sessionID == "" || cwd == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	return resolveClaudeTranscript(dir, sessionID)
}

// Fork origins, by file path. This runs on every search request — per
// keystroke — and reading each candidate's head to find its origin is the
// expensive part; listing the directory is not. A file's origin is fixed
// the moment it is written, so it is cached per path and never goes stale.
//
// Deliberately NOT a memo of the whole answer keyed on the directory's
// mtime, which is what this replaced: a transcript or fork created within
// one mtime tick of a lookup leaves the mtime unchanged, and the stale
// answer — "no transcript yet", or no fork — was served for as long as
// the directory stayed quiet. Windows' coarse directory mtimes exposed it
// in CI; it could happen on any filesystem.
var (
	claudeOriginMu    sync.Mutex
	claudeOriginCache = map[string]string{}
)

// claudeOriginCacheMax bounds the cache. Far above the transcripts one
// project directory holds; exceeding it just starts the cache over.
const claudeOriginCacheMax = 4096

// cachedForkOrigin is claudeForkOrigin with the per-path cache. An empty
// answer is not cached: a fork just created may not have written its first
// session_id record yet, and must be re-read until it has.
func cachedForkOrigin(path string) string {
	claudeOriginMu.Lock()
	origin, ok := claudeOriginCache[path]
	claudeOriginMu.Unlock()
	if ok {
		return origin
	}
	origin = claudeForkOrigin(path)
	if origin == "" {
		return ""
	}
	claudeOriginMu.Lock()
	if len(claudeOriginCache) >= claudeOriginCacheMax {
		claudeOriginCache = map[string]string{}
	}
	claudeOriginCache[path] = origin
	claudeOriginMu.Unlock()
	return origin
}

// resolveClaudeTranscript finds the newest file in dir descending from
// sessionID: its own <sessionID>.jsonl or a later fork of it.
func resolveClaudeTranscript(dir, sessionID string) []string {
	own := filepath.Join(dir, sessionID+".jsonl")
	best, bestMod := "", time.Time{}
	if st, err := os.Stat(own); err == nil {
		best, bestMod = own, st.ModTime()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if best == "" {
			return nil
		}
		return []string{best}
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".jsonl") || name == sessionID+".jsonl" {
			continue
		}
		info, err := ent.Info()
		if err != nil || !info.ModTime().After(bestMod) {
			continue
		}
		p := filepath.Join(dir, name)
		if cachedForkOrigin(p) == sessionID {
			best, bestMod = p, info.ModTime()
		}
	}
	if best == "" {
		return nil
	}
	return []string{best}
}

// claudeForkScanBytes bounds how much of a candidate file is read to find
// its origin. The first record carrying session_id sits ~180 KB into a
// real transcript (preceding records are snapshots and attachments), so
// this leaves ample headroom while keeping a scan of a 20 MB file cheap.
const claudeForkScanBytes = 1 << 20

// claudeForkOrigin returns the snake_case "session_id" of the first
// record in path that carries one — the conversation the file descends
// from — or "" when none appears within claudeForkScanBytes.
func claudeForkOrigin(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, claudeForkScanBytes))
	sc.Buffer(make([]byte, 0, 64<<10), claudeForkScanBytes)
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap pre-filter: most records never mention the field.
		if !bytes.Contains(line, []byte(`"session_id"`)) {
			continue
		}
		var rec struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.SessionID != "" {
			return rec.SessionID
		}
	}
	return ""
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

// --- hook-tier SpawnArgs ---

// claudeHookEvents are the Claude Code hook names Hive wires to
// `hived hook`. See cmd/hived/hook.go for the mapping each becomes.
var claudeHookEvents = []string{
	"SessionStart", "UserPromptSubmit", "Stop", "StopFailure",
	"Notification", "PermissionRequest", "PreToolUse", "PostToolUse",
	"PostToolUseFailure", "SessionEnd", "SubagentStart", "SubagentStop",
}

type claudeHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type claudeHookGroup struct {
	Hooks []claudeHookEntry `json:"hooks"`
}

type claudeSettings struct {
	Hooks map[string][]claudeHookGroup `json:"hooks"`
}

var claudeHivedPathWarnOnce sync.Once

// claudeSpawnArgs is Def.SpawnArgs for Claude: it returns
// `["--settings", <json>]` wiring every event in claudeHookEvents to
// `<hivedPath> hook`, or nil when hooks cannot be wired (no resolved
// hived path, or claude's version is outside the verified range).
//
// --settings hooks CONCATENATE with hooks from other settings sources
// rather than replacing them, so this never has to read the user's own
// settings.json first.
//
// That was verified BY HAND on Claude Code 2.1.260 (a project
// .claude/settings.json Stop hook and a --settings Stop hook both fired
// on one -p turn; see the plan's decision log) and nothing re-checks it
// automatically: cmd/hived/claude_probe_test.go drives a real claude
// but installs only Hive's own hooks, so it cannot see the difference
// between concatenate and replace. If Anthropic ever changes the merge
// semantics, the symptom is the user's own hooks silently not firing in
// Hive sessions, and no test in this repo will catch it.
func claudeSpawnArgs(sp SpawnInfo) []string {
	if !claudeHooksAvailable(sp) {
		return nil
	}
	group := []claudeHookGroup{{Hooks: []claudeHookEntry{
		{Type: "command", Command: claudeHookCommand(sp.HivedPath)},
	}}}
	hooks := make(map[string][]claudeHookGroup, len(claudeHookEvents))
	for _, ev := range claudeHookEvents {
		hooks[ev] = group
	}
	blob, err := json.Marshal(claudeSettings{Hooks: hooks})
	if err != nil {
		log.Printf("agent: marshal claude hooks settings: %v", err)
		return nil
	}
	return []string{"--settings", string(blob)}
}

// claudeHooksAvailable is the single gate for everything Hive adds to
// a Claude spawn: the hook wiring in claudeSpawnArgs, and the task-tool
// opt-in in claudeSpawnEnv. They must agree — the opt-in spends context
// in every session and is only worth it when the hooks are there to
// carry the plan back — and two copies of this check had already
// drifted once (the env side skipped the version gate).
func claudeHooksAvailable(sp SpawnInfo) bool {
	if sp.HivedPath == "" {
		claudeHivedPathWarnOnce.Do(func() {
			log.Printf("agent: hived path could not be resolved; claude sessions run on the heuristic state tier only")
		})
		return false
	}
	return claudeVersionSupportsHooks()
}

// claudeHookCommand builds the shell command line Claude Code runs for
// every hook event. hivedPath is shell-quoted: a macOS app-bundle path
// contains spaces ("Application Support"), and Claude Code invokes hook
// commands through a shell.
func claudeHookCommand(hivedPath string) string {
	return claudeShellQuote(hivedPath) + " hook"
}

// claudeShellQuote wraps s in single quotes, escaping any embedded
// single quote the POSIX-shell way: close the quote, emit an escaped
// quote, reopen it.
func claudeShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// --- Claude version gate ---

// minHooksVersion is the first Claude Code release this integration
// requires: hooks plus --settings merge-not-replace, verified on
// 2.1.260 (see the plan's decision log). Below it, SpawnArgs returns
// nil and the session runs on the heuristic tier only.
const minHooksVersion = "2.1.0"

// maxKnownBadHooksVersion is a release at or above which a hooks
// regression is known to break this integration. Empty means "none
// known yet" — no such regression has been observed, so the upper
// bound is not enforced. Set this (with a decision-log entry naming
// the break) the day one is found; the gate already knows how to use
// it.
const maxKnownBadHooksVersion = ""

var (
	claudeVersionOnce sync.Once
	claudeVersionOK   bool
	claudeVersionSeen string // "unknown" or the parsed leading semver; for logging only

	// claudeVersionProbe runs `claude --version` and returns its
	// output. A var so tests can stub it without forking a real
	// process; production never reassigns it.
	claudeVersionProbe = func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return proc.CommandContext(ctx, "claude", "--version").Output()
	}
)

var claudeSemverRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// claudeVersionSupportsHooks runs the version probe once per daemon
// lifetime (sync.Once) and logs a single line when hooks are disabled
// as a result.
func claudeVersionSupportsHooks() bool {
	claudeVersionOnce.Do(func() {
		claudeVersionOK = probeClaudeVersion()
		if !claudeVersionOK {
			log.Printf("agent: claude version %q is outside the verified hooks range [%s, %s); the hook state tier is disabled for this daemon lifetime",
				claudeVersionSeen, minHooksVersion, orNone(maxKnownBadHooksVersion))
		}
	})
	return claudeVersionOK
}

func orNone(s string) string {
	if s == "" {
		return "∞"
	}
	return s
}

func probeClaudeVersion() bool {
	out, err := claudeVersionProbe()
	if err != nil {
		claudeVersionSeen = "unknown"
		return false
	}
	m := claudeSemverRe.FindSubmatch(out)
	if m == nil {
		claudeVersionSeen = "unknown"
		return false
	}
	v := string(m[0])
	claudeVersionSeen = v
	if semverLess(v, minHooksVersion) {
		return false
	}
	if maxKnownBadHooksVersion != "" && !semverLess(v, maxKnownBadHooksVersion) {
		return false
	}
	return true
}

// semverLess reports whether a < b, comparing major.minor.patch
// numerically (a plain string compare would rank "2.10.0" below
// "2.9.0"). Malformed input compares as 0, which only ever matters for
// a version string this package generated itself via claudeSemverRe.
func semverLess(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	var out [3]int
	parts := strings.SplitN(s, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

// SetClaudeVersionProbeForTest replaces the `claude --version` probe
// and resets the sync.Once gate so the next SpawnArgs call re-probes.
// Test-only; callers must not run with t.Parallel() while the override
// is installed (package-global state).
func SetClaudeVersionProbeForTest(fn func() ([]byte, error)) (restore func()) {
	prevFn := claudeVersionProbe
	claudeVersionProbe = fn
	claudeVersionOnce = sync.Once{}
	return func() {
		claudeVersionProbe = prevFn
		claudeVersionOnce = sync.Once{}
	}
}
