# GUI: scrollback corruption after single↔grid transitions; text overwrites mid-print

- **Issue:** #200
- **Type:** bug
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/200-scrollback-corruption-after-grid-transitions.md](../exec-plans/completed/200-scrollback-corruption-after-grid-transitions.md)

## Problem

After switching from single-session view to grid view and back, scrolling up shows prior output rendered at the narrower grid width (text was reflowed/resized to the grid's column count rather than the current viewport's). Separately, while text is actively printing, lines that are scrolling up the screen sometimes overwrite text that was already there, producing a corrupted scrollback.

Open research questions:
- How are we managing scrollback today (in Hive, the in-house VT, or via xterm.js)?
- Do agents / xterm.js / the virtual terminal already provide scrollback automatically, so we don't need to manage it ourselves?
- What test coverage (Go VT-level + Vitest/jsdom + Playwright per the four-layer harness) can pin scroll/resize behavior down so we can iterate without regressions?

## Desired behavior

- Scrollback content is rendered at the current pane's column width regardless of intermediate resizes (single → grid → single).
- Live output written near the bottom of the viewport scrolls cleanly upward without overwriting already-emitted lines.
- Whichever layer owns scrollback (Hive's VT, xterm.js, the agent's PTY) is intentional and documented, and regressions in its reflow/scroll behavior are caught by automated tests.

## Success criteria

- Repro: open a session in single view, generate enough output to scroll, switch to grid and back, scroll up — prior output renders at the single-view column width.
- Repro: stream rapid output in a session — no line emitted earlier ever gets overwritten by a later line.
- New automated tests at the appropriate layer(s) of the four-layer harness cover both behaviors and fail on the current `main` (or document why a layer can't reach it).

## Non-goals

- Re-implementing the in-house VT emulator's scrollback from scratch (covered by the in-house-vt-emulator plan).
- Changing visible scrollback length / config surface.

## Notes

- Related shipped spec: [143 — vt snapshot: scrollback above visible viewport not preserved](143-vt-snapshot-scrollback-above-visible-viewport.md).
- Related in-flight specs touching xterm rendering: #190 (garbled glyphs), #195 (shared TextDecoder).
- Four-layer test harness landed in 97dfa9a (Go + Vitest + jsdom + Playwright).
