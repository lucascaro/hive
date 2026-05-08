# GUI: resize loses scroll position when viewport is 1-2 lines short of bottom

- **Spec:** [docs/product-specs/163-resize-stick-mostly-bottom.md](../../product-specs/163-resize-stick-mostly-bottom.md)
- **Issue:** #163
- **Stage:** DONE
- **Status:** completed

## Summary

Treat a viewport within 2 lines of the bottom as "at bottom" for the purpose of the post-resize snap in `SessionTerm._onBodyResize`. Fixes the codex case where small TUI scroll deltas leave the viewport just shy of bottom and resize strands the user mid-history.

## Research

`cmd/hivegui/frontend/src/main.js`:
- `_onBodyResize` (line 442) is the single resize entry point (per 561a81c, "unify resize handling under ResizeObserver").
- Line 460 `wasAtBottom = buf.viewportY >= buf.baseY` — strict equality (`viewportY` is bounded `0..baseY`, so `>=` is effectively `===`).
- Line 467: snaps via `term.scrollToBottom()` only when `wasAtBottom`.
- xterm.js v5 buffer API: `viewportY` (top of viewport), `baseY` (top of last screen of scrollback). `baseY - viewportY` is non-negative lines above bottom.

The only other `scrollToBottom()` call (inside the `pty:event scrollback_replay_done` handler around line 1652) is intentionally unconditional — the user hasn't had a chance to scroll yet — and is left untouched.

## Approach

Replace the strict equality with a 2-line tolerance using a named local constant. Tolerance is intentionally small: catches codex's 1–2 line drift without overreaching into deliberate scrollback.

### Files to change

- `cmd/hivegui/frontend/src/main.js` lines 456-460 — introduce `STICKY_BOTTOM_LINES = 2` and compute `wasAtBottom = (buf.baseY - buf.viewportY) <= STICKY_BOTTOM_LINES`.

### Tests

No JS test framework in this project. Verification is manual:

1. `wails dev`. Start a session running codex; reproduce the "1–2 lines off bottom" rest state.
2. Resize the window or drag the sidebar. Expected: viewport snaps to bottom.
3. Scroll ≥ 3 lines up; resize. Expected: scroll position preserved.
4. Scroll exactly to bottom; resize. Expected: stays at bottom (regression check).
5. ⌘\ grid ↔ single toggle while at "mostly bottom". Expected: snaps to bottom.

## Decision log

- **2026-05-08** — Threshold = 2 lines. Reason: matches the codex case the user reported; small enough that deliberate scrollback at 3+ lines is still preserved.

## Progress

- **2026-05-08** — Spec + plan created. Implemented in same commit (S-sized change).
- **2026-05-08** — Branch off latest main (`27430c7`) in worktree `.worktrees/resize-stick`. `go test ./...` green.

## Open questions

None.
