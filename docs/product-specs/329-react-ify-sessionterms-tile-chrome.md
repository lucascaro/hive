---
issue: null
title: "React-ify SessionTerm's tile chrome"
type: enhancement
complexity: L
priority: P3
pr: 334
stage: REVIEW
---

# React-ify SessionTerm's tile chrome

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P3
- **Exec plan:** [master](../exec-plans/active/329-react-ify-sessionterms-tile-chrome.md) · [phase 1](../exec-plans/completed/329-react-ify-sessionterms-tile-chrome-phase1.md) (completed) · [phase 2](../exec-plans/active/329-react-ify-sessionterms-tile-chrome-phase2.md)

## Problem

`cmd/hivegui/frontend/src/app/session-term.ts` (~1700 lines) is the one region
the React rewrite deliberately did not port. It builds the tile — header, state
icon, title, buttons, the dead-session overlay, the replay loading panel — with
`document.createElement`, and it is the last significant caller of the
imperative primitives `src/ui/icon.ts` and `src/ui/icon-button.ts`. So the
frontend still has two rendering paradigms, and the design system still has two
copies of two primitives.

A second, smaller holdout exists that the original filing did not name:
`src/app/banners.ts`'s `wireCheckUpdatesButton()` (added by PR #325) injects an
imperative `iconButton()` into the sidebar header. `src/ui/icon-button.ts`
cannot be deleted while it stands.

## Desired behaviour

The tile *chrome* renders from React; the xterm instance stays imperative behind
a portal boundary, keyed by session id.

## Non-goals

- React must not own the xterm lifecycle. That was considered and rejected in
  the rewrite's Decisions: it re-fights every documented timing fix.
- `keyboard.ts` decomposition and the CSS Modules migration are separate filed
  debt ([keyboard-keymap-tables](keyboard-keymap-tables.md),
  [frontend-css-modules](frontend-css-modules.md)).

## Constraints

These are the reasons this was deferred, not incidental details:

- A `SessionTerm` holds one of **eight process-wide WebGL slots**
  (`src/lib/webgl-budget.ts`), acquired in the constructor and released in
  `destroy()`. Unmount/remount of a mounted terminal is the bug the whole
  migration existed to avoid.
- Hosts are **reparented, never recreated** (`src/app/grid-layout.ts`).
- `ensureAttached()` is not effect-idempotent — it re-latches follow-bottom on
  every call, so an effect must not call it more often than today's paths do.
- The grid template is written **before** attach, or the scrollback restream
  jumps.
- `focusActiveTerm`'s 8-frame retry and `setView`'s 250 ms bottom-snap delay
  (`src/app/focus.ts`, `src/app/view.ts`) both encode shipped bug fixes.
- `.tile-header` is pinned to `height: 28px; flex-shrink: 0`
  (`src/theme/components/tile-header.css`). That fixed box is what makes a
  portal boundary safe: an empty header on the frame before React fills it
  leaves `.term-body`'s box unchanged, so first-attach `fit()` still measures
  the right rows.

## Success criteria

- The tile chrome is a React component tree; `src/ui/` is deleted, with
  `ensureSprite()` / `ICON_NAMES` / `IconName` relocated (they are still needed
  by `components/Icon.tsx`, so `src/ui/icon.ts` is moved, not simply removed)
  and `src/app/banners.ts`'s `wireCheckUpdatesButton()` ported into
  `components/Sidebar.tsx`.
- The `e2e` and `e2e-real` suites pass **unmodified**, including the scroll-jump
  and restream-strand specs. The existing specs already pin the tile's DOM
  contract (`chrome.spec.ts`, `theme.spec.ts`, `minimize.spec.ts`,
  `silent-failures.spec.ts`, `worktrees.spec.ts`, `session-lifecycle.spec.ts`,
  `ux-polish.spec.ts`, `grid-scroll-regressions.spec.ts`); that contract is the
  acceptance gate.
- No terminal is unmounted and remounted by a view change, a reorder, a resize
  or a theme switch, proven by a new e2e spec asserting host-node identity
  across all four.
- `SessionTerm` still creates, owns and destroys `host`, `body`, the xterm
  instance and the WebGL slot. No `SessionTerm` instance enters React state.

## Phases

This spec ships in two PRs (master plan's `## Phases`). The spec's
`## Success criteria` are the gate at **phase 2**, not phase 1 — the same
convention the React rewrite used across its seven phases. `stage:`
therefore tracks the phase in flight, and only phase 2's gate writes
`DONE`.

- **Phase 1** — portal boundary + the tile header. PR
  [#329](https://github.com/lucascaro/hive/pull/329); gate PASS
  2026-09-03; [plan](../exec-plans/completed/329-react-ify-sessionterms-tile-chrome-phase1.md).
- **Phase 2** — the overlays, `src/ui/` deletion, `banners.ts`, docs, the
  changeset. Not started;
  [plan](../exec-plans/active/329-react-ify-sessionterms-tile-chrome-phase2.md).

## Notes

Filed by Phase 6 of the React UI rewrite
([spec](react-ui-rewrite.md)) as a named debt item; the rewrite's Non-goals
section names it explicitly. Renumbered from the original numberless
`sessionterm-react.md` when the work was planned, so `/hs-merge-gate` can
resolve it.

Number 329 skips the local `max+1` (324) on purpose: every other spec number in
this repo is a GitHub issue number, and #324 is already the Phase-6 PR.
