# Hive opens only a shell on Windows, not Claude

- **Issue:** #183
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/183-windows-claude-opens-shell.md](../exec-plans/active/183-windows-claude-opens-shell.md)

## Problem

On Windows, launching a session in Hive that should start Claude (the `claude` CLI) opens a plain shell instead. The expected behavior — matching macOS/Linux — is for the configured agent command (`claude`) to launch directly inside the session terminal.

This appears to be a platform-specific spawning issue: the command resolution, shell wrapping, or PATH handling on Windows is producing a shell rather than executing the agent binary.

## Desired behavior

On Windows, starting a Hive session whose agent is `claude` launches the `claude` CLI inside the session terminal, matching macOS/Linux behavior. Other agents (gemini, copilot, etc.) should also launch correctly on Windows.

## Success criteria

- On Windows, creating a new session with the Claude agent results in the `claude` CLI running, not a bare shell prompt.
- No regression on macOS or Linux for any agent.

## Non-goals

- Installing or configuring the `claude` CLI itself on Windows — assume it is already on PATH.

## Notes

Related to recent Windows fixes (#177, #179).
