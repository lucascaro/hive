# Per-session TextDecoder

- **Spec:** [docs/product-specs/195-shared-textdecoder-glyphs.md](../../product-specs/195-shared-textdecoder-glyphs.md)
- **Issue:** #195
- **Stage:** REVIEW
- **Status:** active

## Summary

Move the streaming UTF-8 decoder from module scope into `SessionTerm` so each session has its own state. Eliminates cross-session contamination of partial multi-byte sequences. Add a unit test that exercises the interleave-at-rune-boundary case.

## Research

- `cmd/hivegui/frontend/src/main.js`
  - Module-level `const decoder = new TextDecoder('utf-8', { fatal: false });` — single instance shared by every session.
  - `SessionTerm.writeData(b64)` calls `decoder.decode(bytes, { stream: true })`. `stream: true` buffers any incomplete multi-byte sequence at the tail of the input until the next call.
  - `writeData` is invoked from the Wails `data` event router. Events from different sessions can arrive arbitrarily interleaved.
- `cmd/hivegui/frontend/test/unit/` — existing Vitest unit tests; the harness already proven on `wire.test.js`, `focus.test.js`, etc.

## Approach

Give `SessionTerm` its own decoder. Each instance owns its streaming state; concurrent sessions cannot corrupt each other. The fix is mechanical (one field + one reference rename + remove the module-level decoder) and matches the rest of the per-session state already on the class (xterm instance, fit addon, ResizeObserver, etc.).

Considered alternative: drop `stream: true` and accept that a chunk ending mid-rune produces one U+FFFD. Rejected — Claude's spinner emits frequent small writes; mid-rune chunks would be common and visibly broken.

### Files to change

- `cmd/hivegui/frontend/src/main.js`
  - `SessionTerm` constructor: assign `this.decoder = new TextDecoder('utf-8', { fatal: false });`.
  - `SessionTerm.writeData`: use `this.decoder` instead of the module-level `decoder`.
  - Remove the module-level `const decoder = ...`.

### New files

- `cmd/hivegui/frontend/test/unit/decoder.test.js` — regression test for per-session decode isolation.

### Tests

- `decoder.test.js`:
  - Two independent `TextDecoder` instances (modeling two `SessionTerm`s) each receive a stream split mid-rune for a multi-byte UTF-8 string (e.g. `'こんにちは'`, `'✻ Working'`, `'┌─┐'`). Assert each reconstructs its original string with no `U+FFFD`.
  - A single shared decoder, fed the same interleaved bytes, *does* produce `U+FFFD` — pinning the bug to verify the test would have caught it.

## Decision log

- **2026-05-14** — Per-session decoder over dropping `stream: true`. Why: Claude's spinner writes are small and frequent; mid-rune chunks are the common case, not the edge case.

## Progress

- **2026-05-14** — Diagnosed root cause, implemented fix, removed global decoder, added regression test, opened PR.

## Open questions

None.

## PR convergence ledger

<!-- Append-only. One line per review-loop iteration. -->

