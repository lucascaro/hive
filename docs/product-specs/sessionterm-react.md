---
issue: null
title: "React-ify SessionTerm (the last imperative render path)"
type: enhancement
complexity: L
priority: P3
stage: TRIAGE
---

# React-ify SessionTerm (the last imperative render path)

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P3
- **Exec plan:** —

## Problem

`cmd/hivegui/frontend/src/app/session-term.ts` (~1700 lines) is the one region
the React rewrite deliberately did not port. It builds the tile — header, state
icon, title, buttons, the dead-session overlay, the replay loading panel — with
`document.createElement`, and it is the only remaining production caller of the
imperative primitives `src/ui/icon.ts` and `src/ui/icon-button.ts`. So the
frontend still has two rendering paradigms, and the design system still has two
copies of two primitives.

## Desired behaviour

The tile *chrome* renders from React; the xterm instance stays imperative behind
a ref boundary, keyed by session id.

## Non-goals

- React must not own the xterm lifecycle. That was considered and rejected in
  the rewrite's Decisions: it re-fights every documented timing fix.

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

## Success criteria

- The tile chrome is a React component; `src/ui/icon.ts` and
  `src/ui/icon-button.ts` are deleted, their last callers ported to
  `components/Icon.tsx` / `components/IconButton.tsx`.
- The `e2e-real` suite passes unmodified, including the scroll-jump and
  restream-strand specs.
- No terminal is unmounted and remounted by a view change, a reorder, a resize
  or a theme switch.

## Context

Filed by Phase 6 of the React UI rewrite
([spec](react-ui-rewrite.md)) as a named debt item; the rewrite's Non-goals
section names it explicitly.
