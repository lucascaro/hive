# Phase 3 — Agent integration

**Status:** In progress
**Branch:** `silent-light`
**Inputs:** `phase-2.md` and the working multi-session daemon at `ce6eeb8`

## Goal

A session can be created as one of:
- a **shell** (default — current Phase 2 behavior)
- an **agent** (Claude, Codex, Gemini, Copilot, Aider, …) — the
  daemon spawns the agent's command directly instead of $SHELL, so
  the user lands inside the agent's REPL, not at a shell prompt

After Phase 3:

> Open a Claude session and a Codex session side-by-side. Run a query
> in each. Quit the GUI. Relaunch. Both sessions are listed with the
> same names + colors and **come back as Claude and Codex**, not as
> generic shells.

The differentiation vs old (tmux) Hive starts here.

## Out of scope

- Agent teams (multiple coordinated agents) — Phase 5 along with
  workflow port
- Status detection (running / waiting for input / idle) — needs hooks
  into xterm output; Phase 5
- Hooks / scripts that run on session events — Phase 5
- Agent installer UX (`npm install -g …`) — surface command but don't
  run it for the user in this phase

## Design

### Built-in agent definitions

```go
type Def struct {
    ID          string   // "claude", "codex", "gemini", "copilot", "aider", "shell"
    Name        string   // "Claude", "Codex", ...
    Cmd         []string // argv to spawn (looked up on $PATH at run time)
    Color       string   // default sidebar color
    InstallCmd  []string // shown when not detected; never auto-run
}
```

Built-in defs (initial set):

| ID      | Name    | Cmd         | Color     | Install                                        |
|---------|---------|-------------|-----------|------------------------------------------------|
| shell   | Shell   | (login shell) | `#9ca3af` | —                                            |
| claude  | Claude  | claude      | `#f59e0b` | `npm install -g @anthropic-ai/claude-code`     |
| codex   | Codex   | codex       | `#10b981` | `npm install -g @openai/codex`                 |
| gemini  | Gemini  | gemini      | `#3b82f6` | `npm install -g @google/gemini-cli`            |
| copilot | Copilot | copilot     | `#8b5cf6` | `npm install -g @github/copilot`               |
| aider   | Aider   | aider       | `#ec4899` | `pip install aider-chat`                       |

Detection: `exec.LookPath(cmd[0])` once at daemon startup and on
demand. Agents not on PATH appear in the launcher with a "not
installed" badge and the install command is shown for copy-paste —
but we do not run `npm install` automatically (per AGENTS.md "no
backwards-compat hacks", and because npm-global installs are
user-environment-sensitive).

### Wire protocol delta

`CreateSpec.Cmd []string` already exists in v1. No wire format change.
We add an optional `Agent string` field so persistence captures *what
kind* of session this is, not just the literal Cmd:

```go
type CreateSpec struct {
    ...
    Cmd []string  `json:"cmd,omitempty"`
    Agent string  `json:"agent,omitempty"`   // Phase 3: e.g. "claude"
}
```

Reasoning: future Phase 5 features (hooks, status detection) want to
know "this is a Claude session" without sniffing the command. And on
revive after daemon restart, we want to re-spawn the same agent — by
ID — even if the user has updated the install location.

`SessionInfo.Agent` is added so the sidebar can show an icon /
distinct color cue without a second roundtrip.

### Daemon

`session.Options.Cmd []string` runs in place of $SHELL when set.
`registry.Create` plumbs both Cmd and Agent through; metadata file
stores `agent` so Revive can re-resolve via `agent.Defs[meta.Agent]`
and pick up the latest installed binary.

### GUI

Sidebar `+` button is replaced with a **launcher menu** — small
floating list of available agents:

```
+----------------------+
|  + Shell             |
|  + Claude            |
|  + Codex             |
|    Gemini  [install] |   ← not on PATH; click reveals install cmd
|  + Copilot           |
|  + Aider             |
+----------------------+
```

Click a row → `CreateSession(name='', color=def.Color, cmd=def.Cmd, agent=def.ID)`.

Cmd+N still creates a default shell (no menu); Cmd+Shift+N opens the
menu (or just makes the menu always the only path — TBD during
implementation, default to "+ button shows menu, Cmd+N defaults to
shell").

## File layout (delta from Phase 2)

```
internal/
  agent/
    agent.go         Def struct, built-in registry, PATH detection
    agent_test.go
  registry/
    registry.go      stores Agent on Entry; Revive resolves via agent pkg
  session/
    session.go       Options.Cmd plumbed to ptmx.Command(...)
  wire/
    control.go       CreateSpec.Agent + SessionInfo.Agent
cmd/hivegui/
  app.go             ListAgents() bound method
  frontend/src/
    main.js          launcher menu, agent-aware create
    style.css        menu styles
```

## Milestones

| #   | Goal | Done when |
|-----|------|-----------|
| 3.1 | `internal/agent` with defs + detection | unit test asserts at least shell+claude defined; LookPath shape |
| 3.2 | session.Options.Cmd runs in place of shell | session test starts `/bin/echo hello`, sees output before EOF |
| 3.3 | wire CreateSpec.Agent + SessionInfo.Agent | round-trip test |
| 3.4 | registry persists & resolves agent on Revive | persistence test reopens, revives agent, command matches def |
| 3.5 | GUI launcher menu | manual smoke; clicking each item creates that agent |
| 3.6 | **Acceptance: Claude+Codex persist** | manual on macOS |

## Acceptance test

1. Build, launch GUI from a project directory.
2. Open the agent launcher (+).
3. Create a Claude session ("scratch", default color).
4. Create a Codex session ("review", default color).
5. Run `hello` in each (whatever the agent expects).
6. Cmd-Q the GUI.
7. Relaunch.
8. **Pass criteria:**
   - Sidebar shows Claude + Codex with their original names + colors
   - Clicking Claude lands you back in the Claude REPL (not bash)
   - Clicking Codex lands you back in the Codex REPL
   - The session metadata file under `~/Library/Application Support/Hive/sessions/<id>/session.json` contains `"agent": "claude"` / `"agent": "codex"`

## Risks

| Risk | Mitigation |
|------|------------|
| Agent binary path changes between launches (e.g. nvm switch) | Resolve via PATH each Revive; not stored as absolute path |
| Some agents require extra env (API keys, etc.) | Phase 3 first cut: rely on user shell rc; Phase 5 may add per-agent env |
| Agent process exits and session.Session has no live PTY anymore | Show dead state in sidebar (already wired in Phase 2 dead-state UI); revive on click is Phase 1.7 territory |
