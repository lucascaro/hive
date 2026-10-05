package agent

import (
	"fmt"

	"github.com/lucascaro/hive/internal/proc"
)

// ACPSpec says how to run an agent as an ACP session (spec 496): the
// adapter command the daemon speaks the Agent Client Protocol to over
// stdio. Adapter versions are pinned so an upstream release cannot
// change what Hive talks to without a Hive release; they are the
// versions spike 492 probed (scripts/acp-probe/agents.mjs).
type ACPSpec struct {
	// Argv is the adapter command. "npx" adapters need Node.js; Hive
	// never bundles or maintains an adapter (spec 496 non-goal).
	Argv []string
	// Experimental marks an agent whose ACP mode spike 492 did not
	// probe. The launcher labels it.
	Experimental bool
	// Modes is the adapter's permission modes, least permissive first.
	// The user's ceiling for the agent (Settings.ACPCeiling) is one of
	// them, and Hive sets every session to a mode at or below it with
	// session/set_mode after each session/new and session/load. Empty
	// means Hive does not know the adapter's modes, so it cannot cap
	// them and ACP stays unavailable (ACPAvailable).
	Modes []ACPMode
	// DefaultMode is the ceiling when the user has not chosen one.
	DefaultMode string
	// ModeOptions maps the permission-card option ids that switch the
	// session's mode to the mode each one sets. A switch above the
	// ceiling stands only when it is the mode of the option the user
	// just picked; every other one is reset (spec 496, phase 3).
	ModeOptions map[string]string
	// NoApprovalGate marks an agent that never asks before using a
	// tool (Pi, F5 in acp-workflows.md). Its Modes are ACPModeOff and
	// ACPModeUnattended, which are Hive's, not the adapter's: no
	// set_mode is sent, and a session starts only at ACPModeUnattended.
	NoApprovalGate bool
}

// ACPMode is one permission mode an adapter advertises.
type ACPMode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// The two ceilings of a NoApprovalGate agent.
const (
	ACPModeOff        = "off"
	ACPModeUnattended = "unattended"
)

// ModeRank is mode's position in Modes (0 = least permissive), or -1
// when the adapter did not advertise it.
func (s *ACPSpec) ModeRank(mode string) int {
	for i, m := range s.Modes {
		if m.ID == mode {
			return i
		}
	}
	return -1
}

// Pinned adapter packages. Bump only after re-running
// scripts/acp-probe against the new version.
const (
	claudeACPPackage = "@agentclientprotocol/claude-agent-acp@0.85.1"
	codexACPPackage  = "@agentclientprotocol/codex-acp@2.1.1"
	piACPPackage     = "pi-acp@0.0.34"
)

// acpSpecs is consulted by Def.ACP; kept apart from defsByID so the
// catalog literal stays readable.
var acpSpecs = map[ID]*ACPSpec{
	// Mode ids as the pinned adapters advertise them (probed
	// 2026-10-04, see the 496 exec plan). Claude's default is Manual,
	// not plan: plan is stricter but cannot edit at all.
	IDClaude: {Argv: []string{"npx", "-y", claudeACPPackage}, DefaultMode: "default", Modes: []ACPMode{
		{"plan", "Plan"}, {"default", "Manual"}, {"acceptEdits", "Accept edits"},
		{"auto", "Auto"}, {"bypassPermissions", "Bypass permissions"},
	}, ModeOptions: map[string]string{
		// The exit-plan card's options (permissions/options/shared.js in
		// the pinned adapter).
		"exit-plan-default":            "default",
		"exit-plan-accept-edits":       "acceptEdits",
		"exit-plan-auto":               "auto",
		"exit-plan-bypass":             "bypassPermissions",
		"exit-plan-clear-accept-edits": "acceptEdits",
		"exit-plan-clear-auto":         "auto",
		"exit-plan-clear-bypass":       "bypassPermissions",
	}},
	IDCodex: {Argv: []string{"npx", "-y", codexACPPackage}, DefaultMode: "read-only", Modes: []ACPMode{
		{"read-only", "Read-only"}, {"workspace-write", "Workspace access"},
		{"agent", "Auto review"}, {"agent-full-access", "Full access"},
	}},
	IDPi: {Argv: []string{"npx", "-y", piACPPackage}, DefaultMode: ACPModeOff, NoApprovalGate: true, Modes: []ACPMode{
		{ACPModeOff, "Off"}, {ACPModeUnattended, "Unattended tool use"},
	}},
	IDGemini:  {Argv: []string{"gemini", "--acp"}, Experimental: true},
	IDCopilot: {Argv: []string{"copilot", "--acp", "--stdio"}, Experimental: true},
}

// ACP returns the agent's ACP spec, or nil when it cannot run as an
// ACP session (the shell, Aider, custom agents).
func (d Def) ACP() *ACPSpec { return acpSpecs[d.ID] }

// loginPATHFn is the PATH ACP availability is judged on: the login
// shell's, which is what the adapter will actually run with. A seam
// because the real probe runs the developer's own rc files.
var loginPATHFn = proc.LoginPATH

// ACPCeiling is the most permissive mode an ACP session of agent id may
// run in: the user's choice when it is one the adapter advertises,
// otherwise the agent's DefaultMode. "" for an agent with no modes.
func (s Settings) ACPCeiling(id ID) string {
	spec := acpSpecs[id]
	if spec == nil {
		return ""
	}
	if m := s.ACPModeCeiling[string(id)]; spec.ModeRank(m) >= 0 {
		return m
	}
	return spec.DefaultMode
}

// ACPRefusal is why the user's settings forbid an ACP session of this
// agent, or "" when they allow one. The daemon checks it at every start
// (create, revive, restart), not only at create, so lowering a setting
// takes effect on the next start.
func (d Def) ACPRefusal(st Settings) string {
	spec := d.ACP()
	switch {
	case spec == nil:
		return fmt.Sprintf("%s does not support ACP sessions", d.Name)
	case len(spec.Modes) == 0:
		return fmt.Sprintf("Hive can't cap %s's permission mode yet, so ACP is off for it", d.Name)
	case spec.NoApprovalGate && st.ACPCeiling(d.ID) != ACPModeUnattended:
		return fmt.Sprintf("%s never asks before using a tool; allow unattended tool use for it in Settings → Agents to use ACP", d.Name)
	}
	return ""
}

// ACPAvailable reports whether an ACP session can be started for this
// agent under st, and when not, a reason fit to show the user. It runs
// the login-shell probe on first use, so never call it under a lock.
func (d Def) ACPAvailable(st Settings) (bool, string) {
	if reason := d.ACPRefusal(st); reason != "" {
		return false, reason
	}
	spec := d.ACP()
	path := loginPATHFn()
	if spec.Argv[0] == "npx" && proc.LookPathIn(path, "npx") == "" {
		return false, "ACP needs Node.js: npx was not found on your login shell's PATH"
	}
	// The adapter drives the agent's own CLI, so that has to be there
	// too; for native adapters Argv[0] is the CLI.
	if len(d.Cmd) > 0 && proc.LookPathIn(path, d.Cmd[0]) == "" {
		return false, fmt.Sprintf("%s was not found on your login shell's PATH", d.Cmd[0])
	}
	return true, ""
}

// ACPEnvPATH returns the PATH an ACP adapter should run with.
func ACPEnvPATH() string { return loginPATHFn() }
