# vt snapshot: CJK / wide-char column misalignment

- **Spec:** [docs/product-specs/142-vt-snapshot-cjk-wide-char-alignment.md](../../product-specs/142-vt-snapshot-cjk-wide-char-alignment.md) (lives on the PR branch only — not yet on main)
- **Issue:** #142
- **PR:** — (#160 was closed unmerged; see Status below)
- **Branch:** `feature/142-vt-snapshot-cjk-wide-char-alignment`
- **Stage:** TRIAGE (back to the top of the pipeline: PR #160 was closed
  unmerged, so nothing is in flight; superseded by `in-house-vt-emulator.md`)
- **Status:** active

## Status as of 2026-08-24

**PR #160 was closed without merging.** The bug is still real: `internal/session/vt.go`
has no wide-cell concept (no `runewidth`/`charmbracelet/x/vt` import; `go.mod` still
pins `hinshun/vt10x`), so CJK and wide-emoji rows still misalign on reattach.

This stub stays in `active/` as the record of the known bug, but it is **not in
flight** — nobody is working the branch. The backend swap it proposed is the same
swap `in-house-vt-emulator.md` scopes more carefully, so treat that plan as the
route to the fix and this file as the symptom report. Do not restart the #160
branch without reconciling the two.

## Summary of what's on the branch

- Swaps the headless emulator behind `RenderSnapshot` from `hinshun/vt10x`
  (dormant, no wide-cell concept) to `charmbracelet/x/vt`, which models cells
  in display columns via `charmbracelet/ultraviolet`. Final CUP in the
  snapshot lands at the same column xterm.js reads from the live byte stream.
- Drops the hand-rolled SGR encoder, reverse-video pre-swap unwind, and
  `lastNonBlank` / `\x1b[K` logic.
- Regenerates all 11 fixtures in `internal/session/testdata/conformance/`
  against the new backend (the corpus from PR #174 was pinned to vt10x).
- Adds wide-char round-trip / cursor-position / overlay tests; tightens
  `scripts/dev-iso.sh` umask; routes test `NewVT` calls through a
  `t.Cleanup`-registering helper so the drainer goroutine doesn't leak.
