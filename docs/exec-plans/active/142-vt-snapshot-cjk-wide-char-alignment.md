# vt snapshot: CJK / wide-char column misalignment

- **Spec:** [docs/product-specs/142-vt-snapshot-cjk-wide-char-alignment.md](../../product-specs/142-vt-snapshot-cjk-wide-char-alignment.md) (lives on the PR branch only — not yet on main)
- **Issue:** #142
- **PR:** #160
- **Branch:** `feature/142-vt-snapshot-cjk-wide-char-alignment`
- **Stage:** REVIEW (PR open, not merged)
- **Status:** active

## Why this stub exists on main

The full exec plan, product spec, code change, conformance-corpus regen, and
tests live on the PR branch. This stub is here so anyone reading `main`'s
`docs/exec-plans/active/` index sees that #142 is in flight on PR #160 and
doesn't start a parallel attempt.

When PR #160 lands, the merge will replace this stub with the real plan and
move it to `completed/`.

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
