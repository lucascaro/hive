---
issue: 553
title: Dictate into a session from anywhere via a global capture HUD
type: enhancement
complexity: L
priority: P2
pr: 556
stage: GATE
---

# Dictate into a session from anywhere via a global capture HUD

- **Issue:** #553
- **Exec plan:** [docs/exec-plans/active/553-dictate-into-a-session-from-anywhere-via-a-global.md](../exec-plans/active/553-dictate-into-a-session-from-anywhere-via-a-global.md)

## Problem

A Hive user who is working in another app (browser, editor) can't send a prompt to a running agent without switching to Hive, finding the session and typing. Dictation tools such as Handy and macOS dictation only paste into the focused window, so voice doesn't help either. Any shortcut that injects text into an agent from outside Hive is risky, because those agents run shell commands. The text could be misrecognised or go to the wrong session.

## Desired behavior

A global hotkey works whether or not Hive is focused. It opens a small floating capture HUD that takes keyboard focus. Any dictation tool (Handy, macOS dictation, or typing) puts text into the HUD. The HUD prominently shows the target session's name and title. The default target is the last-active session, and the user can pick a different session from inside the HUD. Nothing reaches an agent until the user confirms. Confirming submits the text to the chosen session, and Esc discards it with no side effects. The flow is the same when Hive is focused. Hive ships no speech model and asks for no microphone permission.

## Success criteria

- With Hive unfocused, pressing the global hotkey opens the HUD with keyboard focus, and text dictated by Handy or macOS dictation lands in the HUD's input.
- The HUD shows the target session's name and title, and defaults to the last-active session.
- The user can switch the target to any other live session from inside the HUD without opening the main window.
- The text is not written to any session until the user presses confirm. Esc closes the HUD and nothing is written to any session. Both can be checked against the session's PTY input.
- After confirm, the text arrives in the chosen session and is submitted, and only that session receives it.
- The HUD shows its confirm, cancel and switch-target keys. Confirm and cancel are the Keybindings Policy's Enter/Esc overlay exception; target switching uses native ↑/↓ list navigation, recorded as a policy exception in AGENTS.md. The global hotkey itself is a binding, rebindable in Settings → Shortcuts.
- Pressing the hotkey with no live sessions shows a clear empty state and never writes anywhere.
- The same hotkey and HUD flow works when Hive is focused.
- Hive adds no new socket, CLI or IPC entry point that accepts text for a session. All injection goes through the human-confirmed HUD.

## Non-goals

- Voice commands that control Hive (switch, approve, status).
- Always-listening or wake-word capture. Capture starts only from the hotkey.
- Bundling a speech model or requesting microphone access in Hive.
- Remote or phone input. Only the local user is covered.
- A programmatic or scriptable "send to session" entry point.
- Platforms other than macOS for v1.

## Notes

Open questions carried from `/hs-brainstorm`:

- The default global hotkey, and whether it is rebindable through `keymap.json`. Global OS hotkeys sit outside the app keymap today.
- Using Handy takes two hotkeys: Hive's to open the HUD, then Handy's to dictate. Could that be one press? This needs research into Handy's CLI and signals (https://handy.computer/docs/cli).
- Whether the HUD lives in `hivegui` or `hivebar`, since `hivebar` already runs while the GUI is unfocused.
- How multi-line text is handled on submit (bracketed paste vs Enter).
- Prior art: Claude Code `/voice` and macOS dictation work only in the focused terminal. Handy: https://github.com/cjpais/handy
