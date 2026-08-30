---
issue: null
title: "GUI: unbind ⌘/Ctrl+Enter from the grid toggle"
type: bug
complexity: S
priority: P2
stage: DONE
pr: 287
shipped: 2026-08-28
---

# GUI: unbind ⌘/Ctrl+Enter from the grid toggle

- **Issue:** —
- **PR:** #287
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Shipped:** 2026-08-28
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/249-unbind-cmd-enter-grid-toggle.md](../exec-plans/completed/249-unbind-cmd-enter-grid-toggle.md)

## Problem

⌘Enter (Ctrl+Enter off macOS) currently toggles single ⇄ grid-project, mirroring ⌘G. That is the wrong owner for the chord: Enter belongs to the focused agent session, and the capture-phase window handler (`cmd/hivegui/frontend/src/app/keyboard.ts:334`) swallows the event before xterm ever sees it, so ⌘Enter is unusable inside a session. Users pressing it in Claude or Codex get an unwanted view change instead of the terminal behavior they expect. ⌘G and ⇧⌘G already cover both grid toggles, so the alternate binding buys nothing.

## Desired behavior

⌘/Ctrl+Enter has no app-level meaning. Pressing it in a session does not change the view; whatever xterm does with a meta-modified Enter (in practice: nothing) is what happens. ⌘G / ⇧⌘G remain the grid toggles, and Shift+Enter's newline behavior (#217) is untouched.

## Success criteria

- ⌘/Ctrl+Enter in single mode does not switch to grid-project.
- ⌘/Ctrl+Enter in grid mode does not maximize to single.
- ⌘G still toggles grid-project and ⇧⌘G still toggles grid-all.
- Shift+Enter still sends `0x0a`; plain Enter still sends `\r`.
- The "Toggle Grid (⌘↩ alternate)" View-menu item is gone on macOS.
- The shortcuts panel no longer lists ↩ / Enter as a grid toggle.

## Non-goals

- Giving ⌘Enter a replacement behavior (submit, newline, or anything else).
- Changing ⌘G / ⇧⌘G, Shift+Enter, or plain Enter.
- Making the binding configurable.

## Notes

Touch points: `cmd/hivegui/frontend/src/app/keyboard.ts:334` (the branch), `cmd/hivegui/frontend/src/lib/shortcuts.ts:150` (help panel row), `cmd/hivegui/menu_darwin.go:91` plus `menu_darwin_test.go`, and the README shortcut table. Related: #217, which documented ⌘Enter as the grid toggle and routed newline insertion to Shift+Enter instead — that reasoning is now obsolete for ⌘Enter but Shift+Enter stays.

Numbering: no GitHub issue was created; 248 was skipped because it is already a PR number.
