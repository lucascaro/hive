# Fix grid-mode regressions: scrollback baseline + sidebar focus

- **Issue:** #208
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/completed/208-grid-regressions-scrollback-focus.md](../exec-plans/completed/208-grid-regressions-scrollback-focus.md)
- **PR:** #209
- **Shipped:** 2026-05-16

## Problem

Three regressions in grid mode, all reported together:

- **R1** — The first time the user enters grid mode after restarting Hive, scrolling in tiles is broken (scrollback appears wrong or gets overwritten mid-print).
- **R2** — After minimizing a session in grid mode (PR #202/#204), scrolling in the remaining tiles is broken in the same way.
- **R3** — After resizing the window, toggling the sidebar (⌘S off/on) prevents typing in the focused terminal — keystrokes don't reach the active xterm.

These regressions made it to `main` despite the scrollback baseline work in #200/#203 and the unified-focus work in #181/#182 + #186/#189, because the test harness (#188) had no coverage for: (a) first-attach baseline init in grid, (b) baseline drift when grid reflows for non-resize reasons (minimize/restore), and (c) focus survival across sidebar-toggle layout reflow.

## Desired behavior

- Scrolling works in every grid tile on first entry after restart.
- Scrolling continues to work in remaining tiles after one tile is minimized; restoring a minimized session does not break scroll in any tile.
- Toggling the sidebar with ⌘S after a window resize leaves the active terminal focused; keystrokes flow uninterrupted.
- Regression tests (unit + Playwright e2e) cover all three so they cannot quietly recur.

## Success criteria

- Manual smoke (operator): restart Hive → grid-all → scroll up/down in each tile → scrollback intact, no mid-print overwrite.
- Manual smoke: 3 sessions in grid → minimize one → scroll in the other two → scrollback intact; restore the minimized one → scroll in all three.
- Manual smoke: resize window → ⌘S off → ⌘S on → type → keystrokes appear in the active tile.
- Automated: `npm test` passes including new `replay-baseline.test.js` and extended `scrollback.test.js`.
- Automated: `npm run test:e2e` passes including new `grid-scroll-regressions.spec.js` and `sidebar-focus-regression.spec.js`.
- The existing ≥4-col window-resize replay path is preserved (e2e R-control case asserts it).

## Non-goals

- Daemon-side scrollback ring changes — root cause is frontend.
- Cross-launch persistence of minimized state.
- Wider focus-management refactor across single ↔ grid (covered by #181/#186).

## Notes

Adjacent prior work: #200/#203 (scrollback baseline), #202/#204 (minimize), #181/#182 (unified focus), #186/#189 (grid focus regression), #188 (test harness).
