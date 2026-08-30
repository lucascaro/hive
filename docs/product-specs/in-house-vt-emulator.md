---
issue: null
title: "In-house VT emulator (replace hinshun/vt10x)"
type: enhancement
complexity: L
priority: P2
stage: IMPLEMENT
---

# In-house VT emulator (replace hinshun/vt10x)

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/in-house-vt-emulator.md](../exec-plans/active/in-house-vt-emulator.md)

## Problem

Session-reattach snapshots are built on `github.com/hinshun/vt10x`, unmaintained
since 2022-03-01 (`go.mod` pin `v0.0.0-20220301184237-5011da428d02`). Its data
model leaks into our code in ways we cannot fix upstream: `vt.go:20-29` mirrors
unexported attribute constants, `writeSGR` (`vt.go:418-461`) undoes two storage
transforms on every render, and it has no scrollback API — so our ring is fed by
`captureEvictions` (`vt.go:121-149`), a heuristic that reverse-engineers
evictions from row diffs and has needed two fixes already. Five vt10x-specific
fixes landed in four days (#141, #142, #158, #162, `337a5fe`, `499bbc5`).

Users reattaching to sessions feel this as misrendered agent TUIs (Claude, Codex,
vim, htop, less) that paint with mixed SGR, wide chars, and scrollback.

## Desired behavior

A minimal terminal model we own, sized to what the snapshot path actually needs,
with a conformance corpus that pins behavior on the real apps users run inside
hive.

## Success criteria

- No dependency on `hinshun/vt10x` in the snapshot path.
- No mirrored unexported constants and no storage-transform undo in render.
- A real scrollback API replaces the eviction heuristic.
- A conformance corpus covers the agent TUIs users actually run.

## Non-goals

- A general-purpose terminal emulator. Scope is the snapshot path only.
- Phases 1–3 — deferred pending Phase 0 outcomes.

## Notes

Spec written retroactively (2026-08-30); the plan carried an interim
`## Problem (interim spec)` section, now superseded by this file.
