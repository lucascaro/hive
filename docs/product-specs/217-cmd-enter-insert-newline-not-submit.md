# GUI: Shift+Enter should insert a newline, not submit, in agent sessions

- **Issue:** #217
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/217-cmd-enter-insert-newline-not-submit.md](../exec-plans/active/217-cmd-enter-insert-newline-not-submit.md)

## Problem

Inside an agent session (Claude, Codex), there is no way to add a line break to a multi-line prompt from the GUI — every Enter variant submits. xterm sends a bare `\r` for Enter and drops the Shift modifier, so the agent CLI cannot distinguish Shift+Enter from plain Enter and submits the command. Users expect Shift+Enter — the universal "newline in a chat input" convention — to add a line break so they can compose a multi-line prompt, then submit with Enter.

The original report named Cmd+Enter, but Cmd+Enter (and Ctrl+Enter on non-mac) is already a documented app shortcut: it toggles single ↔ grid-project view via a **capture-phase** window keydown handler (`cmd/hivegui/frontend/src/main.js:2768`) that `stopPropagation()`s the event before it ever reaches the terminal. Shift+Enter carries no Cmd/Ctrl modifier, so that handler ignores it and the key reaches xterm — making Shift+Enter the correct, conflict-free key for newline insertion.

## Desired behavior

Pressing Shift+Enter in an agent session inserts a newline into the agent's input buffer (the same effect the agent's own multi-line shortcut produces), without submitting. Plain Enter continues to submit the command unchanged. Cmd/Ctrl+Enter continues to toggle grid-project view.

## Success criteria

- In a Claude or Codex session, Shift+Enter adds a line break to the input and does not submit (it sends Ctrl+J / `0x0a` to the PTY).
- Plain Enter still submits the current input (sends `\r` / `0x0d`).
- Behavior is identical across platforms (Shift+Enter is not platform-gated).
- Cmd/Ctrl+Enter still toggles the grid-project view (unchanged).

## Non-goals

- Reassigning Cmd/Ctrl+Enter, which remains the grid-project toggle.
- Remapping Option+Enter or the CSI-u Shift+Enter terminal encoding.
- Changing newline behavior for agents that do not support multi-line input.

## Notes

Related key handling lives in `cmd/hivegui/frontend/src/main.js:243` (`attachCustomKeyEventHandler`, where Shift+Enter is intercepted) and the new `cmd/hivegui/frontend/src/lib/keymap.js` (`isShiftEnter`, `NEWLINE_SEQ`). The conflicting Cmd/Ctrl+Enter grid-project toggle is at `main.js:2768` inside the capture-phase window handler.
