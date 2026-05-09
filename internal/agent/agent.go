// Package agent describes the AI-coding agents Hive can launch in a
// session (Claude, Codex, Gemini, Copilot, Aider) plus a plain shell.
// The daemon spawns CreateSpec.Cmd directly; this package gives the
// GUI and the daemon a shared catalog of "what does X require" so we
// don't sprinkle agent names through the codebase.
package agent

import (
	"context"
	"os/exec"
	"sort"
	"sync"
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
	defsMu   sync.RWMutex
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
	}

	// displayOrder is the order shown in the launcher.
	displayOrder = []ID{IDShell, IDClaude, IDCodex, IDGemini, IDCopilot, IDAider}
)

// Get returns the def for id, or zero Def + false if unknown.
func Get(id ID) (Def, bool) {
	defsMu.RLock()
	defer defsMu.RUnlock()
	d, ok := defsByID[id]
	return d, ok
}

// All returns every built-in agent in display order.
func All() []Def {
	defsMu.RLock()
	defer defsMu.RUnlock()
	out := make([]Def, 0, len(displayOrder))
	for _, id := range displayOrder {
		if d, ok := defsByID[id]; ok {
			out = append(out, d)
		}
	}
	return out
}

// Available returns the subset of agents whose binary is on PATH.
// Useful for surfacing only-installed agents at the top of the menu.
func Available() []Def {
	all := All()
	out := make([]Def, 0, len(all))
	for _, d := range all {
		if d.Available() {
			out = append(out, d)
		}
	}
	return out
}

// SortByName returns defs sorted alphabetically by display name.
// Used when an alternative ordering is desired (e.g. command-palette).
func SortByName(in []Def) []Def {
	out := append([]Def(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
