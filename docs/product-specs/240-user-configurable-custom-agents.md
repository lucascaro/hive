# Add user-configurable custom agents

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/240-user-configurable-custom-agents.md](../exec-plans/completed/240-user-configurable-custom-agents.md)

## Problem

The set of tools Hive can launch is a hardcoded Go map (`defsByID`, `internal/agent/agent.go:71`) covering shell, claude, codex, gemini, copilot, aider, and pi. Anyone who wants to launch a variant — a `claude-lite` that runs `claude --model haiku`, a locally-built CLI, or any of these agents with different default flags — has to edit Go source and rebuild the app. There is no user-facing configuration surface of any kind: Hive has a state directory but no config file, and the GUI has no settings screen.

## Desired behavior

A user can define their own agents from inside the app and launch them like any built-in. A Settings screen — opened with the standard **⌘,** shortcut, or from **File ▸ Settings…** — lists their custom agents and lets them add, edit, and delete entries with a display name, a command line, and a color. Saved agents appear immediately in the ⌘T new-session dropdown alongside the built-ins, with the same availability check (greyed out with an install hint when the binary is not on `PATH`). Launch, persistence, and command-based revival across restarts match built-in behavior; conversation resume is the one exception and is explicitly not supported for custom agents (Restart re-runs the command rather than continuing the prior session).

## Success criteria

- **⌘,** opens a Settings screen, also reachable from **File ▸ Settings…** and the command palette; Ctrl+, opens it on Windows/Linux.
- A custom agent added in Settings appears in the ⌘T launcher dropdown without restarting Hive.
- Launching a custom agent spawns its configured command with its configured arguments.
- A session running a custom agent survives a quit/relaunch — it revives with the same command.
- Renaming a custom agent does **not** break revive for sessions already created with it.
- A custom agent whose binary is missing from `PATH` renders as unavailable, matching built-in behavior.
- Attempting to save an agent that collides with a built-in ID, has an empty command, or duplicates another custom ID is rejected with a visible inline error.
- A hand-corrupted `agents.json` never prevents the launcher from opening; built-ins still list and a warning is logged.

## Non-goals

- **Resume / `--continue` support for custom agents.** Built-ins carry Go function fields (`ResumeArgs`, `CaptureSessionIDFn`) that a JSON config cannot express. Restarting a custom-agent session re-runs its base command rather than resuming the prior conversation.
- **Per-project agent overrides.** Custom agents are global to the user.
- **Importing or sharing agent definitions** between users or machines.
- **Overriding a built-in agent's definition.** Built-ins win ID collisions.
- **A separate OS-level settings window.** Wails v2 is one webview per process; the Settings screen is an in-window panel reached by a native menu item.
- **Settings sections beyond agents.** The screen is structured so more can be added later but ships with one.

## Notes

Prior art in this repo: `launcher.js` already renders whatever `ListAgents` returns, so no launcher changes are needed once custom defs merge at the `agent.Get`/`agent.All` chokepoint.

Numbering note: this spec is 240 rather than 218 because GitHub's issue/PR sequence had already reached #239, and 218 is an existing PR number.
