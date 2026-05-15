# GUI: scrollback corruption after single↔grid transitions; text overwrites mid-print

- **Spec:** [docs/product-specs/200-scrollback-corruption-after-grid-transitions.md](../../product-specs/200-scrollback-corruption-after-grid-transitions.md)
- **Issue:** #200
- **Stage:** DONE
- **Status:** completed
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

> **Note (post-implementation):** This approach was specified before the eng review surfaced that ripping out `historyRows` + `captureEvictions` would cascade through `RenderSnapshot`, the conformance corpus, `scripts/vtcapture`, and every snapshot-based test. The shipped implementation adds the byte ring *alongside* the legacy row-history ring rather than replacing it; `SubscribeAtomicSnapshot` and `RenderSnapshot` remain in place. Cleanup of the legacy path is tracked as a follow-up — see [Decision log](#decision-log).

Two distinct bugs, two fixes, one shared piece of plumbing.

**Fix 1 — narrow scrollback after resize.** Add a per-session **raw-byte ring buffer (default 8 MiB, hard-coded const `ringCap = 8<<20`)** in the Go VT alongside the existing row-history ring. On overflow, drop oldest bytes preferring a newline boundary, falling back to ESC, then to UTF-8 lead bytes verified to be outside any unterminated CSI / OSC sequence (`insideUnterminatedEscape` helper). The vt10x screen state stays — it still computes "what's on screen right now". The byte ring becomes the source of truth for "everything that happened".

Add a new wire frame `FrameRequestReplay` (`internal/wire/frame.go`). The daemon streams ring bytes back chunked at 16 KiB, bracketed by two new events `ScrollbackReplayBegin` / `ScrollbackReplayDone`. Initial attach uses this same path, replacing the old `RenderSnapshot`-based attach data write — fixes cross-attach scrollback truncation (previously capped at the 500-row vt10x history).

Frontend: `_onBodyResize` (`cmd/hivegui/frontend/src/main.js`) sends a debounced (100 ms) `FrameRequestReplay` when `cols` change crosses a ≥4-col threshold **relative to the baseline at the last replay** — not relative to the previous measurement, so a 80→90→89 sequence still triggers a replay even though no single step is ≥4. The frontend stays a dumb pipe — no client-side buffer.

**Fix 2 — live text overwriting scrollback.** Symptom is the snapshot+live race in the attach path: snapshot bytes and live PTY bytes interleaved at xterm with no buffer reset between. Two new Session methods serialize replay against fanout:

- `SubscribeWithAtomicReplay(sink, writeFn)` — initial attach. Captures the ring, calls writeFn (which writes Begin → bytes → Done under the sink's f.mu), then registers the sink, all under s.mu. Deliver is blocked for the duration; live fanout to this sink starts strictly after Done.
- `EmitAtomicReplay(writeFn)` — mid-session `FrameRequestReplay`. Sink already registered; s.mu blocks deliver while writeFn runs; queued live bytes deliver in order after Done.

Frontend handles `ScrollbackReplayBegin` by calling `term.reset()` (via the extracted `handleScrollbackEvent` helper for unit-test access). Wire-order — not a JS-side phase flag — is what guarantees no live bytes land between Begin and Done.

### Why this design

- **Single source of truth.** Bytes live only in the daemon ring. No client-side dup with xterm's cell scrollback.
- **Composes.** Fix 2's gating events carry fix 1's replay traffic too.
- **No cell-level reflow needed.** Replaying raw bytes lets xterm re-parse at the new width — sidesteps the "vt.go:166 says it's not worth the complexity" problem.
- **Memory:** 8 MiB × typical 3–10 sessions = 24–80 MiB in the daemon. The cap is currently a Go `const`, not env/config-tunable; raising it requires a recompile.

### Files to change

1. `internal/wire/frame.go` — add `FrameRequestReplay` frame type (0x14).
2. `internal/wire/control.go` — add `EventScrollbackReplayBegin` / `EventScrollbackReplayDone` event constants.
3. `internal/session/vt.go` — add a raw-byte ring (cap `ringCap = 8<<20`, hard-coded) alongside the existing `historyRows` ring. Add `RingBytes()` accessor and `appendRing` with newline-preferring, CSI-safe boundary trim. **Legacy `historyRows` ring and `captureEvictions` are retained** — load-bearing for `RenderSnapshot` and the conformance corpus. Cleanup tracked as follow-up.
4. `internal/session/session.go` — add `SubscribeWithAtomicReplay` and `EmitAtomicReplay`. Both hold s.mu across snapshot+writeFn so deliver is serialized.
5. `internal/daemon/daemon.go` — handle the new `FrameRequestReplay` via `EmitAtomicReplay`. Initial attach uses `SubscribeWithAtomicReplay`. `frameSink.writeReplay` writes Begin → chunked ring bytes → Done under f.mu (called from inside the s.mu-held writeFn, so lock order is s.mu → f.mu, matching deliver).
6. `cmd/hivegui/app.go` — add `RequestScrollbackReplay(id)` Wails binding.
7. `cmd/hivegui/frontend/src/main.js` — `_onBodyResize` tracks `_replayBaselineCols`; on every resize, clear pending timer; re-arm only if current cols are ≥4 off the baseline. On replay events, delegate to `handleScrollbackEvent`.
8. `cmd/hivegui/frontend/src/lib/scrollback.js` — `shouldRequestReplay` and `handleScrollbackEvent` helpers, unit-testable.

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

## QA verdict

- **2026-05-15** — verdict: NEEDS_FOLLOWUP; checks: 4 PASS / 0 FAIL / 1 followup; followups: #206 (gofmt trailing newline in daemon.go); one-line: spec's three success criteria all met by code-review + tests; one trailing-newline gofmt regression flagged as cosmetic followup.
  - 2026-05-15 dimensions:
    - build/lint/test — PASS — `go build ./...`, `go vet ./...`, `go test ./...`, Vitest 85/85, `npm run build` all green. gofmt -l surfaced 10 files but 9 are pre-existing drift unrelated to #200; the one new offender (internal/daemon/daemon.go) is a single trailing newline → tracked as #206.
    - acceptance — PASS — Criterion 1 (narrow-scrollback-after-resize) and Criterion 2 (no-overwrite-mid-print) verified by code-walk of the s.mu→f.mu lock-ordered replay path; Criterion 3 (test coverage at appropriate layers) satisfied by 5 new Go tests + 14 Vitest cases + daemon TestRequestReplay, despite some test-name drift from the plan.
    - non-goals — PASS — vt10x history ring left intact alongside the additive byte ring; no scrollback-length config surface added.
    - regression — PASS — wire 0x14 is client→server optional; frontend `pty:event` handler tolerates unknown kinds via `handleScrollbackEvent` no-op return; `SubscribeAtomicSnapshot` retained and still tested; lock order s.mu→f.mu uniform across deliver and new APIs; ring lifetime bound to Session (no leak).
    - doc accuracy — PASS — CHANGELOG #200 entry is accurate and user-visible; doc comments on new wire/session/daemon symbols are present; plan + spec correctly updated; AGENTS.md/README correctly untouched.

## Decision log

- **2026-05-15** — Plan called for replacing `historyRows` ANSI ring + dropping `captureEvictions`. During IMPLEMENT the cascade through `RenderSnapshot` → conformance tests → `scripts/vtcapture` was assessed as out-of-scope for this PR. Decision: add the byte ring alongside the legacy ring, leave `RenderSnapshot` and the snapshot-based reattach API intact (still used by `SubscribeAtomicSnapshot`). Follow-up to delete the legacy path when the conformance corpus is migrated. Why: keeps this PR focused on the two scrollback bugs without entangling a snapshot-format migration.
- **2026-05-15** — `ringCap` is hard-coded at 8 MiB (`const ringCap = 8 << 20` in vt.go). Plan originally said "tunable via env/config" — that was aspirational and is not implemented. If operators need to tune, raise via a follow-up: read from `os.Getenv("HIVE_SCROLLBACK_RING_KB")` at `NewVT` time. Decision deferred because the 8 MiB default has not yet been observed to be too small or too large in practice.

## PR convergence ledger

- **2026-05-15 iter 1** — verdict: REQUEST_CHANGES; findings: 1 BLOCKING (replay snapshot race), 3 IMPORTANT (CSI-unsafe trim boundary; dead phase field; legacy history ring follow-up); action: autofix+push (manual, sub-agent did not invoke hivesmith skills); head_sha: fb1579e.
- **2026-05-15 iter 2** — verdict: REQUEST_CHANGES (BLOCKING closed; 11 unresolved Copilot threads); action: manual autofix of 6 real findings (debounce baseline, binding rename, handler extract+test, plan drift in 3 places) and resolve-with-rationale of 11 threads; head_sha: de9dbe6.
- **2026-05-15 iter 3** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 2b9cdb6.

## Progress

- **2026-05-15** — Spec created, issue #200 opened, triage: bug / L / P2. Stage → RESEARCH.
- **2026-05-15** — Research complete. Stage → PLAN.
- **2026-05-15** — Plan approved via interactive HTML review (3 revisions: v1 truncate-scrollback → v2 client-side ring → v3 daemon-side byte ring, approved). Approval flag: `~/.claude/plans/scrollback-corruption-after-grid-transitions.approved.json`. Stage → IMPLEMENT.
- **2026-05-15** — Implementation landed on `feature/200-scrollback-corruption-after-grid-transitions`. All Go + Vitest tests pass (74/74 frontend, full Go suite). PR #203 open. Stage → REVIEW.

## Open questions

- Does xterm.js's built-in scrollback (`scrollback` option) handle reflow on resize, or do we override it?
- Does the in-house VT (`internal/session/vt.go`) maintain its own scrollback that competes with xterm.js's?
- Is the data path "PTY → Go VT → snapshot → xterm.write" or "PTY → bytes → xterm.write" directly? The answer determines who reflows.
