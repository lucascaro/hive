# Fix huge-text flash on grid → zoom → session switch (regression)

- **Issue:** #176
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/176-huge-text-flash-zoom-session-switch.md](../exec-plans/active/176-huge-text-flash-zoom-session-switch.md)

## Problem

When transitioning from grid view to a zoomed session and then switching to another session, a huge-text flash still appears briefly before the terminal renders at the correct size. This regresses the fix shipped in #168 (7f1f78a) — that fix addressed the grid→zoom path, but the flash returns when subsequently switching to a different session from the zoomed view.

## Desired behavior

No oversized text flash during the zoom → session-switch transition. The terminal of the newly selected session should render at its final size on the first paint after the switch.

## Success criteria

- Switching between zoomed sessions shows no transient huge-text frame.
- Repeatable on the same machines/OS where #168 was originally observed.

## Non-goals

- Refactoring the show()/fit() flow beyond what's needed to fix this regression.
- Changes to the grid view rendering itself.

## Notes

- Prior fix: 7f1f78a (#168). That commit forced a synchronous fit in `show()` for the grid→zoom path. The session-switch path likely takes a different code path or hits xterm's WebGL canvas-resize-on-rAF on a different trigger.
- Likely files: `cmd/hivegui/frontend/src/main.js`.
