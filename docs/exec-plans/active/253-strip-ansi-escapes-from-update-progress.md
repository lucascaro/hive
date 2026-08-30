# Strip ANSI escape sequences from update build progress lines

- **Spec:** [docs/product-specs/253-strip-ansi-escapes-from-update-progress.md](../../product-specs/253-strip-ansi-escapes-from-update-progress.md)
- **Issue:** —
- **PR:** #296
- **Branch:** feature/253-strip-ansi-escapes-from-update-progress
- **Status:** active

## Summary

Sanitize `build.sh` output before it becomes an update progress message, so the
green update banner shows plain text instead of raw ANSI escape sequences.

## Research

Authored via plan-first mode. Relevant code:

- `cmd/hivegui/update_apply_darwin.go` — `runBuildScript` scans `./build.sh`
  stdout+stderr and calls `progress(line)` per line; `tail` feeds the
  `build.sh failed: %s` error.
- `cmd/hivegui/update_action.go:121` — progress snapshots are emitted to the
  frontend as `update:progress`.
- `cmd/hivegui/frontend/src/lib/update-state.ts` — maps `info.message` to the
  banner status string; `banners.ts` / `settings.ts` render it as text.
- `cmd/hivegui/update_apply_other.go` has no build shell-out; nothing to fix.

## Approach

Strip terminal control sequences at the single point where child-process output
enters the update path — `runBuildScript` — rather than defensively in the
frontend. One helper applied to both the progress callback and the failure tail.

A hand-rolled scanner instead of a regexp: the state machine is short, runs per
output line, and avoids a package-level regexp for a one-caller helper.

Handled cases: `\r` redraws (keep the segment after the last `\r`), CSI
(`ESC [ … final byte`), OSC (`ESC ] … BEL` or `ESC \`), and any leftover C0
control byte except tab.

### Files to change

- `cmd/hivegui/update_apply_darwin.go` — add `plainProgressLine(string) string`;
  apply it in `runBuildScript`'s scan loop so both `progress(line)` and `tail`
  carry the sanitized value.

### New files

None.

### Tests

- `TestPlainProgressLineStripsTerminalControls` — table test in
  `cmd/hivegui/update_shellout_darwin_test.go`.
- `TestRunBuildScriptReportsSanitizedProgress` — stub `./build.sh` emitting an
  ANSI-colored line; assert no `\x1b` reaches the progress callback.

## Decision log

- **2026-08-30** — Sanitize in Go, not in the frontend. Why: single choke point,
  and the error string benefits too.

## Progress

- **2026-08-30** — Plan-first scaffold; stage = IMPLEMENT.
- **2026-08-30** — Implemented, tests green, PR #296 opened; stage = REVIEW.

## Open questions

None.

## PR convergence ledger

- **2026-08-30 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 05bec86.
