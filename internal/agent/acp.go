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
	IDClaude:  {Argv: []string{"npx", "-y", claudeACPPackage}},
	IDCodex:   {Argv: []string{"npx", "-y", codexACPPackage}},
	IDPi:      {Argv: []string{"npx", "-y", piACPPackage}},
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

// ACPAvailable reports whether an ACP session can be started for this
// agent, and when not, a reason fit to show the user. It runs the
// login-shell probe on first use, so never call it under a lock.
func (d Def) ACPAvailable() (bool, string) {
	spec := d.ACP()
	if spec == nil {
		return false, fmt.Sprintf("%s does not support ACP sessions", d.Name)
	}
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
