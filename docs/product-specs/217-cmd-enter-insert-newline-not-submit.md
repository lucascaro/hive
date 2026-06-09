# GUI: Cmd+Enter should insert a newline, not submit, in agent sessions on macOS

- **Issue:** #217
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/217-cmd-enter-insert-newline-not-submit.md](../exec-plans/active/217-cmd-enter-insert-newline-not-submit.md)

## Problem

On macOS, pressing Cmd+Enter inside an agent session (Claude, Codex) submits the current command immediately instead of inserting a newline into the input. Users expect Cmd+Enter to add a line break so they can compose multi-line prompts, then submit separately. The terminal sends a bare `\r` for Enter and does not encode the Cmd modifier, so the agent CLI cannot distinguish Cmd+Enter from a plain Enter. The GUI's custom key handler (`attachCustomKeyEventHandler` in `cmd/hivegui/frontend/src/main.js`) already intercepts other macOS combos but does not handle Cmd+Enter, so it falls through to xterm's default submit behavior.

## Desired behavior

Pressing Cmd+Enter in an agent session inserts a newline into the agent's input buffer (the same effect the agent's own multi-line shortcut produces), without submitting. Plain Enter continues to submit the command unchanged.

## Success criteria

- In a Claude or Codex session on macOS, Cmd+Enter adds a line break to the input and does not submit.
- Plain Enter still submits the current input.
- Behavior on non-macOS platforms is unchanged.

## Non-goals

- Remapping any other modifier+Enter combination (Shift+Enter, Option+Enter).
- Changing newline behavior for agents that do not support multi-line input.

## Notes

Related key handling lives in `cmd/hivegui/frontend/src/main.js:242` (`attachCustomKeyEventHandler`) and `cmd/hivegui/frontend/src/lib/platform.js`.
