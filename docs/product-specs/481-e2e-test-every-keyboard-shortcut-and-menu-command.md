---
issue: 481
title: "E2E-test every keyboard shortcut and menu command from the binding data"
type: enhancement
complexity: M
priority: P2
pr: 486
stage: GATE
---

# E2E-test every keyboard shortcut and menu command from the binding data

- **Issue:** #481
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/481-e2e-every-shortcut.md](../exec-plans/active/481-e2e-every-shortcut.md)

## Problem

The e2e suites press only the common shortcuts. About a dozen bindings are never exercised end to end:

- ⌘= / ⌘+ (zoom in)
- Ctrl+` (open terminal)
- session back/forward (⌃- on mac, Ctrl+Alt+- elsewhere)
- the activity grid (⇧⌘J, or Ctrl+Alt+Shift+J)
- ⇧⌘P, ⇧⌘N, ⇧⌘W, ⇧⌘⌫
- ⌘B / ⇧⌘B
- ⌘[ / ⌘]
- ⌘3 to ⌘9
- the reserved ⇧⌘Z

Only 4 of about 46 `menu:*` events are fired, and on macOS those events, not keydowns, are the real entry point for ⌘ chords. #478 made bindings data (`KEY_SCOPES` in `app/key-scopes.ts`, `MENU_COMMANDS` in `app/commands.ts`), and its parity test proves that each key reaches the right command id. Nothing proves end to end that the running app dispatches every chord and menu event.

## Desired behavior

A table-driven Playwright spec, run against the mock-bridge e2e app, is **generated from the binding data**, so a new binding or menu item is covered without anyone writing a test for it:

- On the platform under test, the spec presses every chord of every scope in `KEY_SCOPES`, not only the always-active `app` scope. That includes the modal scopes (e.g. `settings`, `worktrees`), `inline-rename`, `choice-dialog`, `blocked-tile`, `dead-overlay` and `plugins`. A per-scope fixture makes the scope active (opens the modal, starts the rename, blocks or kills the session, loads a plugin with a bound command) before its chords are pressed. The spec asserts that the expected command id ran. A scope that has bindings but no fixture fails the spec, so a new scope cannot go untested.
- It fires every `MENU_COMMANDS` event and asserts its command id ran.
- A **test-only command log** records each dispatch as the command id plus the boolean `runCommand` returned (`false` = declined). It is enabled only by a flag the e2e harness sets, and it is absent and inert in production builds.
- Reserved chords (`command: null`) run nothing and leave the key unconsumed.
- Declining commands (⌘⏎ in single view, ⌘9 past the last session) are asserted as declined: logged with `false`, and the key is left unconsumed.
- The fixture fixes the session count. Each generated `switch-N` chord and `menu:switch-N` event then has a known expected result: accepted where session N exists, and declined past the last session.

Real-effect spot checks cover the shortcuts no e2e test presses today: zoom in, Ctrl+`, back/forward, the activity grid, ⇧⌘P, ⇧⌘N, ⇧⌘W, ⇧⌘⌫, ⌘B/⇧⌘B, ⌘[/⌘] and ⌘3–⌘9. Each asserts the visible result, or the mock-bridge call for commands that open OS windows or terminals.

## Success criteria

- One e2e spec iterates the bindings and `MENU_COMMANDS` rather than hand-listing them. Adding a binding to `key-scopes.ts` with no new test still gets it pressed, and a binding whose command is broken (or wrongly id'd) fails the spec.
- The command log exists only when the e2e flag is set. A test or build check proves the production bundle neither records nor exposes it.
- Every currently unexercised shortcut listed in Problem has a real-effect assertion.
- The spec runs in CI's e2e layer (`scripts/test.sh e2e`) on Linux and passes. It also covers the mac chord set with a run that makes the app see a mac platform before any module loads. For example, `addInitScript` can override `navigator.platform`, as `file-links.spec.ts` already does. `isMac` is computed once at import (`lib/platform.ts`), so sending ⌘ (Meta) events to a non-mac page only tests the non-mac bindings. It does not count as mac coverage.

## Non-goals

- No change to bindings, commands or dispatch behaviour. This is test coverage only.
- No e2e-real (real daemon) version of the full sweep. The mock-bridge layer is the target; e2e-real keeps its current spot coverage.
- No native-menu automation of the actual macOS menu bar. Menu events are fired through the bridge the way the Wails runtime delivers them.

## Notes

Follow-up to #478 (PR #480). The parity unit test there covers chord-to-id matching. This spec covers dispatch through the running app.
