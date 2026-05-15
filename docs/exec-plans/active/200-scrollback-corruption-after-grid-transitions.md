# GUI: scrollback corruption after single↔grid transitions; text overwrites mid-print

- **Spec:** [docs/product-specs/200-scrollback-corruption-after-grid-transitions.md](../../product-specs/200-scrollback-corruption-after-grid-transitions.md)
- **Issue:** #200
- **Stage:** REVIEW
- **Status:** active
- **PR:** https://github.com/lucascaro/hive/pull/203
- **Branch:** feature/200-scrollback-corruption-after-grid-transitions

## Summary

Diagnose two scrollback symptoms in the GUI: (1) after single→grid→single, scrolling up shows prior output wrapped at the narrower grid column count; (2) live output sometimes overwrites lines that have already scrolled. Before proposing a fix, establish *who owns scrollback today* (in-house Go VT, xterm.js, or both) and lock the answer in with the four-layer test harness so iterations are safe.

## Research

### Who owns scrollback (the answer)

**xterm.js owns the live scrollback. The Go VT owns a small parallel ring used only for snapshot-on-reattach.** They do not coordinate.

- Frontend: `cmd/hivegui/frontend/src/main.js:81–98` instantiates `new Terminal({ scrollback: 5000, ... })`. Raw PTY bytes arrive as base64 from the daemon, decoded once, and handed to `term.write(...)` at `main.js:596–601`. **No custom scrollback code in the frontend.**
- Backend: `internal/session/vt.go:31–50` keeps a 500-row ANSI-encoded history ring (`historyRows = 500`). `session.go:204` writes every PTY byte into the VT mirror; `session.go:252` renders a snapshot on reattach (`vt.RenderSnapshot()`); `session.go:272` calls `vt.Resize()` when the GUI resizes.
- Two scrollback buffers exist simultaneously: xterm.js (5000 lines, lives) and Go VT (500 lines, snapshot only). They are not deduped, not aligned, and not reflowed.

### Bug 1 — narrow scrollback after single → grid → single

Smoking gun at `cmd/hivegui/frontend/src/main.js:1227–1238, 1325–1382` (single ↔ grid view) and `:535–570` (resize handler):
- The same xterm.js Terminal instance is reused across modes. A ResizeObserver on each tile body fires `fit.fit()` which calls `term.resize(cols, rows)`.
- xterm.js reflows only the *visible viewport* on resize. Hard-wrapped lines already pushed into scrollback stay at the old (grid) width.
- The Go VT acknowledges the same limitation explicitly at `internal/session/vt.go:166–177`: "History rows captured at the old width remain in the ring as-is — re-laying them out is not worth the complexity since most users don't resize mid-session."

Effect: spending any time in grid view bakes narrow lines into the xterm scrollback. Returning to single view shows them at the old narrow width forever.

### Bug 2 — text overwrites already-scrolled lines during live print

Two distinct mechanisms, both plausible — needs PLAN-stage confirmation via repro tests:

1. **VT eviction race** at `internal/session/vt.go:121–149` (`captureEvictions`). The heuristic matches post-write rows back against the pre-write snapshot to detect how far the screen scrolled. If a `Resize()` interleaves with `deliver()` (or if the terminal contains repeated rows like blank lines), `rowsEqualTerm` can mis-match and lose / re-emit rows. The Go VT history isn't what the user sees on screen, but on reattach the snapshot replays from there.

2. **Snapshot + live double-feed**. On reattach, `daemon.go:420–429` writes the full snapshot (history + visible grid) into the wire, then the live PTY stream resumes. There is no marker telling xterm.js "discard your existing buffer". If a tile was already attached during a transition, xterm.js can receive an overlapping prefix and stamp it over the cursor/last-line.

The "live text overwrites scrollback while printing" symptom matches (2) more strongly because it does not require any resize event.

### Existing test coverage

- Go VT (`internal/session/vt_test.go`, `internal/session/conformance_*_test.go`): snapshot round-trip, scrollback cap at 500, clear-doesn't-leak, resize-with-non-empty-history. **No reflow-on-resize test, no scroll-while-printing test, no snapshot+live race test.**
- Frontend Vitest + jsdom (`cmd/hivegui/frontend/test/unit,dom/`): focus, visibility, renderer recovery, view normalization, grid layout dims. **No reflow / scrollback / mode-transition tests.**
- Playwright e2e (`cmd/hivegui/frontend/test/e2e/`): focus spec only. **Could host a real-xterm reflow regression test** (this is the layer where xterm.js scrollback width is observable).

### Design doc

Detail kept here under 200 lines; no separate design doc needed yet.

### Open questions for PLAN

1. xterm.js v5 does have a reflow API for *soft-wrapped* lines. Do we get soft-wraps or hard newlines from agents (claude / codex / etc.)? If hard newlines dominate, reflow won't help — we'd need to clear-and-replay scrollback on resize.
2. Cheapest fix for bug 1: clear xterm scrollback (`term.clear()` + replay from Go VT snapshot) on mode transitions where width changes meaningfully — accepts losing some history at the cost of correct rendering.
3. Cheapest fix for bug 2: gate snapshot delivery with an explicit "buffer reset" sentinel (xterm escape `\x1b[2J\x1b[3J\x1b[H` or a Hive-side `term.reset()` before snapshot replay), so live and snapshot can't interleave.
4. Do we even need the Go VT's 500-row history for the v2 GUI? It's snapshot-only. If we move that to "let xterm.js own scrollback end to end" we delete a class of bugs (but lose snapshot fidelity if the GUI client connects fresh).

## Approach

Two distinct bugs, two fixes, one shared piece of plumbing.

**Fix 1 — narrow scrollback after resize.** Replace the Go VT's 500-row ANSI history ring (`internal/session/vt.go:31–50`) with a per-session **raw-byte ring buffer (default 8 MiB)** in the daemon. On overflow, drop oldest bytes at a safe CSI / UTF-8 boundary. The vt10x screen state stays — it still computes "what's on screen right now". The byte ring becomes the single source of truth for "everything that happened".

Add a new wire frame `FrameRequestReplay` (`internal/wire/control.go`). The daemon streams ring bytes back through the same chunked path as today's reattach snapshot, bracketed by two new events `ScrollbackReplayBegin` / `ScrollbackReplayDone` (introduced for fix 2, reused here). Initial attach uses this same path, replacing today's `RenderSnapshot`-based reattach — fixes cross-attach scrollback truncation as a free side-effect.

Frontend: `_onBodyResize` (`cmd/hivegui/frontend/src/main.js:535–570`) sends a debounced (100 ms) `RequestReplay` when `cols` change crosses a ≥4-col threshold. The frontend stays a dumb pipe — no client-side buffer.

**Fix 2 — live text overwriting scrollback.** Symptom is the snapshot+live race at `internal/daemon/daemon.go:420–429`: snapshot bytes and live PTY bytes interleave at xterm with no buffer reset between. Add per-sink `replayInFlight` gating in the daemon (live fanout buffers until `Done` ships). Frontend handles `ScrollbackReplayBegin` by calling `term.reset()` before accepting replay bytes. Defensive: also fix the `captureEvictions` heuristic (`vt.go:121–149`) — once we delete the row-history ring, this heuristic goes away entirely.

### Why this design

- **Single source of truth.** Bytes live only in the daemon ring. No client-side dup with xterm's cell scrollback.
- **Composes.** Fix 2's gating events carry fix 1's replay traffic too.
- **No cell-level reflow needed.** Replaying raw bytes lets xterm re-parse at the new width — sidesteps the "vt.go:166 says it's not worth the complexity" problem.
- **Memory:** 8 MiB × typical 3–10 sessions = 24–80 MiB in the daemon. Old ANSI ring was ~50 KiB; net add is bounded and capped via env/config.

### Files to change

1. `internal/wire/control.go` — add `FrameRequestReplay` frame type; add `EventScrollbackReplayBegin` / `EventScrollbackReplayDone` event constants.
2. `internal/session/vt.go` — replace `historyRows` ANSI ring (lines 31–50) with a raw-byte ring (cap configurable; default 8 MiB). Add `RingBytes() []byte` and `RingLen() int` accessors. Drop `captureEvictions` (121–149) and its callers — no longer needed. Keep the vt10x screen for current-state rendering. Add a `resizeInFlight` guard around `Resize` (166–177) for safety.
3. `internal/session/session.go` — write every chunk to the new ring in `deliver` (around 204). Ensure `vt.Resize` and `vt.Write` ordering is unambiguous under `s.mu`.
4. `internal/daemon/daemon.go` — handle the new `FrameRequestReplay`. Emit `Begin` → chunked ring bytes → `Done`. Gate per-sink live fanout via a `replayInFlight` boolean. Initial attach (around 420–429) uses this same path instead of `RenderSnapshot`.
5. `cmd/hivegui/frontend/src/main.js` — in `_onBodyResize` (535–570) detect width-threshold crossing, debounce 100 ms, send `RequestReplay`. New event handler for `ScrollbackReplayBegin` → `term.reset()`. No client-side buffer.
6. `cmd/hivegui/frontend/src/lib/` — if a small helper is warranted for the replay-state machine, factor it out for unit-testability.

### New files

- `cmd/hivegui/frontend/test/unit/scrollback.test.js` — Vitest, threshold + reset behavior.
- `cmd/hivegui/frontend/test/e2e/scrollback.spec.js` — Playwright, real xterm reflow + streaming regression.

### Tests

**Go unit (`internal/session/vt_test.go`):**
- `TestVT_RingCapturesAllBytes` — write N chunks, `RingBytes()` equals concat.
- `TestVT_RingOverflowDropsAtSafeBoundary` — overflow a small ring, surviving prefix starts at `ESC` / UTF-8 lead.
- `TestVT_ReplayReproducesScreen` — write, snapshot screen, reset vt10x, replay ring; screen equals snapshot.
- `TestVT_ResizeWhileWriting_NoEvictionRace` — interleave `Write` and `Resize`; ring is correct.

**Go integration (`internal/daemon/daemon_test.go`):**
- `TestDaemon_ReplayGating_LiveBuffersUntilDone` — sink sees `Begin` → replay → `Done` → live, no interleave.
- `TestDaemon_RequestReplay_StreamsRingBytes` — issue request, bytes equal `vt.RingBytes()`.
- `TestDaemon_InitialAttachReplaysRing` — fresh attach receives full ring before live.

**Vitest + jsdom (`cmd/hivegui/frontend/test/unit/scrollback.test.js` — new):**
- `width-threshold resize sends RequestReplay` — fire resize obs with ≥4-col delta, assert one bridge message after 100 ms debounce.
- `BeginScrollbackReplay resets xterm before replay` — fire event, assert `term.reset()` called before subsequent `term.write`.
- `sub-threshold resizes do not request replay` — 1–2 col delta, no request.

**Playwright e2e (`cmd/hivegui/frontend/test/e2e/scrollback.spec.js` — new):**
- `single → grid → single keeps wide-column scrollback` — drive real xterm in Wails dev shell, scroll up after transit, assert row length ≥ wide-cols − 10 before any soft-wrap.
- `rapid streaming never overwrites scrolled history` — feed `seq 1 10000`, scroll up mid-stream, assert visible row numbers are monotonically increasing — no overwrite.

## Decision log

## Progress

- **2026-05-15** — Spec created, issue #200 opened, triage: bug / L / P2. Stage → RESEARCH.
- **2026-05-15** — Research complete. Stage → PLAN.
- **2026-05-15** — Plan approved via interactive HTML review (3 revisions: v1 truncate-scrollback → v2 client-side ring → v3 daemon-side byte ring, approved). Approval flag: `~/.claude/plans/scrollback-corruption-after-grid-transitions.approved.json`. Stage → IMPLEMENT.
- **2026-05-15** — Implementation landed on `feature/200-scrollback-corruption-after-grid-transitions`. All Go + Vitest tests pass (74/74 frontend, full Go suite). PR #203 open. Stage → REVIEW.

## Open questions

- Does xterm.js's built-in scrollback (`scrollback` option) handle reflow on resize, or do we override it?
- Does the in-house VT (`internal/session/vt.go`) maintain its own scrollback that competes with xterm.js's?
- Is the data path "PTY → Go VT → snapshot → xterm.write" or "PTY → bytes → xterm.write" directly? The answer determines who reflows.
