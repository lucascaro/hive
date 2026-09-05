// Package agent describes the AI-coding agents Hive can launch in a
// session (Claude, Codex, Gemini, Copilot, Aider, Pi) plus a plain shell.
// The daemon spawns CreateSpec.Cmd directly; this package gives the
// GUI and the daemon a shared catalog of "what does X require" so we
// don't sprinkle agent names through the codebase.
package agent

import (
	"context"
	"os/exec"
	"time"
)

// ID is the canonical short identifier ("claude", "codex", ...).
type ID string

// Built-in IDs.
const (
	IDShell   ID = "shell"
	IDClaude  ID = "claude"
	IDCodex   ID = "codex"
	IDGemini  ID = "gemini"
	IDCopilot ID = "copilot"
	IDAider   ID = "aider"
	IDPi      ID = "pi"
)

// Def describes one agent.
type Def struct {
	ID         ID       // canonical identifier; persisted in session metadata
	Name       string   // display name shown in the launcher
	Cmd        []string // argv; first element is resolved via PATH at spawn
	ResumeCmd  []string // argv used by Restart; falls back to Cmd if empty
	Color      string   // default sidebar color
	InstallCmd []string // shown to the user when not detected; never auto-run
	// SessionIDFlag, when non-empty, is appended to Cmd at first spawn as
	// `[flag, sessionID]` so the agent records its conversation under the
	// caller-chosen id. Required for unambiguous Restart when multiple
	// sessions share a cwd/worktree.
	SessionIDFlag string
	// ResumeArgs builds the resume argv for a specific session id. cwd
	// is the directory the session will be respawned in; agents that
	// only have an on-disk transcript after the first user message
	// (Claude) use it to detect the "restarted before any message"
	// case and re-pin under the same id instead of failing with
	// "No conversation found". When nil, Restart falls back to
	// ResumeCmd (path-scoped, ambiguous when sessions share cwd) and
	// then to Cmd.
	ResumeArgs func(sessionID, cwd string) []string
	// CaptureSessionIDFn, when non-nil, is invoked from a goroutine
	// after the agent process is spawned. It returns the agent CLI's
	// session id (e.g. parsed from a rollout file) so future Restart
	// can resume by id. Used for agents that do not accept a
	// caller-chosen id at first launch (codex). The returned id is
	// persisted on the registry Entry; an error or empty string means
	// "no capture this run" and Restart falls back to ResumeCmd.
	CaptureSessionIDFn func(ctx context.Context, cwd string, spawnedAt time.Time) (string, error)
	// SpawnArgs, when non-nil, returns extra argv appended at first
	// spawn AND on every resume/restart (after
	// SessionIDFlag/ResumeArgs/ResumeCmd) — the hook-tier wiring for
	// agents that support it. nil for every agent but Claude and Pi.
	// Only the SpawnInfo fields an adapter actually reads are guaranteed
	// non-empty; an empty field means "unavailable, skip your surface"
	// rather than an error.
	SpawnArgs func(sp SpawnInfo) []string
}

// SpawnInfo is what an adapter may need at spawn time to build
// SpawnArgs. The session id is deliberately not included: neither
// adapter needs it, since Claude gets it via SessionIDFlag and Pi via
// its own --session-id.
type SpawnInfo struct {
	// HivedPath is the absolute path of the running daemon binary
	// (os.Executable(), resolved through filepath.EvalSymlinks once at
	// daemon start); "" if it could not be resolved.
	HivedPath string
	// StateDir is registry.StateDir() for this daemon.
	StateDir string
}

// Available reports whether the agent's binary is on PATH right now.
// Always true for the shell agent (the daemon picks one).
func (d Def) Available() bool {
	if d.ID == IDShell || len(d.Cmd) == 0 {
		return true
	}
	_, err := exec.LookPath(d.Cmd[0])
	return err == nil
}

var (
	defsByID = map[ID]Def{
		IDShell: {
			ID:    IDShell,
			Name:  "Shell",
			Cmd:   nil, // empty → daemon uses default shell
			Color: "#9ca3af",
		},
		IDClaude: {
			ID:            IDClaude,
			Name:          "Claude",
			Cmd:           []string{"claude"},
			ResumeCmd:     []string{"claude", "--continue"},
			Color:         "#f59e0b",
			InstallCmd:    []string{"npm", "install", "-g", "@anthropic-ai/claude-code"},
			SessionIDFlag: "--session-id",
			ResumeArgs:    claudeResumeArgs,
			SpawnArgs:     claudeSpawnArgs,
		},
		IDCodex: {
			ID:         IDCodex,
			Name:       "Codex",
			Cmd:        []string{"codex"},
			ResumeCmd:  []string{"codex", "resume", "--last"},
			Color:      "#10b981",
			InstallCmd: []string{"npm", "install", "-g", "@openai/codex"},
			ResumeArgs: func(id, _ string) []string {
				return []string{"codex", "resume", id}
			},
			CaptureSessionIDFn: codexCaptureSessionID,
		},
		IDGemini: {
			ID:            IDGemini,
			Name:          "Gemini",
			Cmd:           []string{"gemini"},
			ResumeCmd:     []string{"gemini", "--continue"},
			Color:         "#3b82f6",
			InstallCmd:    []string{"npm", "install", "-g", "@google/gemini-cli"},
			SessionIDFlag: "--session-id",
			ResumeArgs: func(id, _ string) []string {
				return []string{"gemini", "--resume", id}
			},
		},
		IDCopilot: {
			ID:         IDCopilot,
			Name:       "Copilot",
			Cmd:        []string{"copilot"},
			ResumeCmd:  []string{"copilot", "--resume"},
			Color:      "#8b5cf6",
			InstallCmd: []string{"npm", "install", "-g", "@github/copilot"},
			ResumeArgs: func(id, _ string) []string {
				return []string{"copilot", "--resume=" + id}
			},
			CaptureSessionIDFn: copilotCaptureSessionID,
		},
		IDAider: {
			ID:         IDAider,
			Name:       "Aider",
			Cmd:        []string{"aider"},
			Color:      "#ec4899",
			InstallCmd: []string{"pip", "install", "aider-chat"},
		},
		IDPi: {
			ID:        IDPi,
			Name:      "Pi",
			Cmd:       []string{"pi"},
			ResumeCmd: []string{"pi", "-c"}, // fallback: continue most recent in cwd
			Color:     "#06b6d4",
			// Pi's own recommended install line keeps --ignore-scripts.
			InstallCmd: []string{"npm", "install", "-g", "--ignore-scripts", "@earendil-works/pi-coding-agent"},
			// `pi --session-id <id>` uses an exact, caller-chosen id,
			// "creating it if missing" and reusing it (no fork) on later
			// launches — verified against pi 0.79.3, which accepts our
			// uuid4 ids. So, like Claude/Gemini, Hive pins its own entry
			// id at first spawn and Restart resumes the same conversation
			// by id; this is unambiguous even when sibling sessions share
			// a cwd/worktree. The one flag both pins and resumes, so
			// ResumeArgs reuses it (and ResumeCmd `pi -c` stays as the
			// fallback for sessions launched with a user-supplied Cmd).
			SessionIDFlag: "--session-id",
			ResumeArgs: func(id, _ string) []string {
				return []string{"pi", "--session-id", id}
			},
			SpawnArgs: piSpawnArgs,
		},
	}

	// displayOrder is the order shown in the launcher.
	displayOrder = []ID{IDShell, IDClaude, IDCodex, IDGemini, IDCopilot, IDAider, IDPi}
)

// Get returns the def for id, or zero Def + false if unknown.
//
// Built-ins are checked first: a custom agent may not shadow one, both
// because built-ins carry ResumeArgs/CaptureSessionIDFn funcs that a
// JSON config cannot express, and so that a bad config can never
// break a built-in agent.
func Get(id ID) (Def, bool) {
	if d, ok := defsByID[id]; ok {
		return d, true
	}
	for _, d := range customDefs() {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

// All returns every available agent — built-ins in display order,
// followed by the user's custom agents in config-file order.
func All() []Def {
	custom := customDefs()
	out := make([]Def, 0, len(displayOrder)+len(custom))
	for _, id := range displayOrder {
		if d, ok := defsByID[id]; ok {
			out = append(out, d)
		}
	}
	return append(out, custom...)
}
