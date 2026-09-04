package agent

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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

// --- hook-tier SpawnArgs ---

// claudeHookEvents are the Claude Code hook names Hive wires to
// `hived hook`. See cmd/hived/hook.go for the mapping each becomes.
var claudeHookEvents = []string{
	"SessionStart", "UserPromptSubmit", "Stop", "StopFailure",
	"Notification", "PermissionRequest", "PostToolUse", "SessionEnd",
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
// rather than replacing them (verified on Claude Code 2.1.260 — see
// the plan's decision log and scripts/probe-claude.sh), so this never
// has to read the user's own settings.json first.
func claudeSpawnArgs(sp SpawnInfo) []string {
	if sp.HivedPath == "" {
		claudeHivedPathWarnOnce.Do(func() {
			log.Printf("agent: hived path could not be resolved; claude sessions run on the heuristic state tier only")
		})
		return nil
	}
	if !claudeVersionSupportsHooks() {
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
		return exec.CommandContext(ctx, "claude", "--version").Output()
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
