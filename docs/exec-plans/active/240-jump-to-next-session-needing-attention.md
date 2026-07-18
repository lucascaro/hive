# GUI: Jump to next session needing attention (⌘B / ⇧⌘B)

- **Spec:** [docs/product-specs/240-jump-to-next-session-needing-attention.md](../../product-specs/240-jump-to-next-session-needing-attention.md)
- **Issue:** —
- **Stage:** REVIEW
- **PR:** #241
- **Branch:** feature/240-jump-to-next-session-needing-attention
- **Status:** active

## Summary

Adds ⌘B (next session needing attention) and ⇧⌘B (jump back to where you were) to the GUI.
Everything needed already exists — `state.attention` holds the flagged session ids, `switchTo`
handles cross-project and cross-view targeting, and `setActive` clears the attention flag on
focus. The work is one pure selector, one return-slot in state, one thin action, and the
lockstep wiring across keyboard / native menu / command palette / help overlay that this repo
requires for every binding.

## Research

Authored via plan-first mode. Code references identified during planning:

- `cmd/hivegui/frontend/src/app/state.js:14` — `attention: new Set()`, session ids with unread
  bells. Source of truth for "needs attention".
- `cmd/hivegui/frontend/src/app/events.js:31` — `onSessionBell` adds to the set and fires the
  desktop notification; `clearAttention` removes.
- `cmd/hivegui/frontend/src/app/focus.js:25` — `setActive` already deletes the id from
  `state.attention`, strips the `.attention` class, and syncs `state.currentProjectId`. No new
  clearing logic needed on the jump path.
- `cmd/hivegui/frontend/src/app/view.js` — `switchTo(id)` retargets `state.gridProjectId` when
  the target belongs to another project, then re-renders single or grid and calls
  `updateSidebarSelection()`. Verified it handles the cross-project / grid cases this feature
  needs, so the jump requires no view-specific branching.
- `cmd/hivegui/frontend/src/app/selectors.js` — `orderedSessions()` gives the
  (project order, session order) list navigation already uses.
- `cmd/hivegui/frontend/src/app/keyboard.js:132` — capture-phase window keydown handler; the
  `⌘W`/`⌘S`/`⌘G` chain is where the new branch goes. `menuActions` map at :325.
- `cmd/hivegui/frontend/src/lib/shortcuts.js` — single source of truth for the help overlay
  (`shortcutGroups`) and the palette's shortcut column (`paletteShortcuts`).
- `cmd/hivegui/menu_darwin.go:83` — the `Session` submenu. `menu_other.go` returns `nil` by
  design (non-mac shortcuts are frontend-only), so nothing to add there.
- `cmd/hivegui/frontend/src/style.css:306,524` — existing `.attention` pulse animations for
  sidebar rows and grid tiles; unchanged.

## Approach

One pure selector + one thin action, wired into every surface that must stay in lockstep.

The return slot is a **single id, not a stack**, holding the session you were working in before
the **first** ⌘B. It is written only when the slot is empty, so a round of bells that walks you
through several flagged sessions still returns you to the work you interrupted rather than to
the previous interruption; ⇧⌘B releases the anchor, which starts the next round. A stack was
rejected as speculative — one anchor covers the glance-and-return flow.

Deliberately **not** mirroring `navSession`'s `if (state.view !== 'single') gridSpatialMove(...)`
split: attention-jump is view-independent — it always targets a specific session, and
`switchTo` already brings it into view regardless of mode or project.

### Files to change

- `cmd/hivegui/frontend/src/app/state.js` — add `attentionReturnId: null` next to `attention`.
- `cmd/hivegui/frontend/src/app/selectors.js` — add pure `nextAttentionId()`: walk
  `orderedSessions()` cyclically from the active index, return the first id in
  `state.attention`, else `null`. Skips the active session; `null` on an empty list.
- `cmd/hivegui/frontend/src/app/keyboard.js` — export `jumpToAttention()` and `jumpBack()`;
  add the `⌘B` / `⇧⌘B` branch to the window keydown handler; register
  `menu:next-attention` / `menu:jump-back` in `menuActions`.
- `cmd/hivegui/frontend/src/main.js` — two `paletteCommands` entries: `next-attention`
  ("Next Session Needing Attention") and `jump-back` ("Jump Back to Previous Session").
- `cmd/hivegui/frontend/src/lib/shortcuts.js` — new items in the `Sessions` group of
  `shortcutGroups`, plus `'next-attention'` / `'jump-back'` in `paletteShortcuts`.
- `cmd/hivegui/menu_darwin.go` — two items in the `Session` submenu using
  `keys.CmdOrCtrl("b")` and `keys.Combo("b", keys.ShiftKey, keys.CmdOrCtrlKey)`.

### New files

- `cmd/hivegui/frontend/test/unit/selectors.test.js` — `selectors.js` has no test file today;
  it is a pure module over the importable `state` object, so it tests with no DOM.

### Tests

`selectors.test.js`, `describe('nextAttentionId')`:

- `returns null when no session has attention`
- `returns null for an empty session list`
- `finds the next flagged session after the active one`
- `wraps past the end of the list`
- `skips the active session even when it is flagged`

`shortcuts.test.js` already asserts no duplicate key combos per group and that every item has
keys + a label — the new `Sessions` entries are covered by those existing invariants.

## Decision log

- **2026-07-18** — ⌘B over ⌘J / ⌘'. Why: "next Bell" mnemonic; unbound today and not reserved
  by macOS. Operator's choice at the plan gate.
- **2026-07-18** — ⇧⌘B is "jump back to where I was", not "previous flagged session". Why:
  operator clarified the intent mid-plan; the glance-and-return flow is what makes the forward
  jump safe to use.
- **2026-07-18** — Single return slot, not a stack. Why: YAGNI; the glance-and-return flow
  needs exactly one hop.
- **2026-07-18** — The slot is written once per round (only when empty), NOT rewritten on every
  ⌘B. Briefly changed to one-hop-back during implementation on review feedback, then reverted:
  the operator confirmed the intent is "the session I was working on before the FIRST ⌘B".
  Trade-off accepted: if a round is never closed with ⇧⌘B, the anchor survives until the next
  ⇧⌘B, which may point at a session from much earlier.
- **2026-07-18** — Numbered 240, not 218. Why: no GitHub issue was created and the shared
  GitHub issue/PR number space is at 239; 218 is an existing PR.

- **2026-07-18** — `nextAttentionId` skips the active session by explicit id check, not by
  relying on it being last in the cyclic walk. Why: the walk runs `i = 1..n`, so the final
  step lands back on the active session and returned it when it was the only flagged one.
  Caught by `returns null when only the active session is flagged`.
- **2026-07-18** — Both new test files live in `test/dom/`, not `test/unit/`. Why: they import
  `state.js`, which reads `localStorage` at module load; the `unit` vitest project runs in the
  node env and throws on import. `test/dom/` is the repo's jsdom project.

## Progress

- **2026-07-18** — Plan-first scaffold; stage = IMPLEMENT.
- **2026-07-18** — Implemented on `feature/240-jump-to-next-session-needing-attention`.
  All six planned surfaces touched + CHANGELOG. 198 frontend tests pass (15 new across
  `test/dom/selectors.test.js` and `test/dom/attention-jump.test.js`); `go build ./...`,
  `go vet ./...`, `gofmt` clean.
  Note: `vite build` cannot run in a fresh worktree — `src/bridge.js` imports the
  `wailsjs/` bindings that only `wails build`/`wails dev` generates. Pre-existing environment
  limitation, unrelated to this change; the Go embed of `frontend/dist` fails for the same
  reason until a wails build runs.
- **2026-07-18** — PR #241 opened; stage = REVIEW. Final state: 199 frontend tests (16 new).

## Open questions

None. Known ceiling (not blocking): on Linux/Windows this claims Ctrl+B from the terminal
(tmux prefix, readline backward-char), following the existing Ctrl+T/W/S/G precedent.
