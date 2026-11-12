# Sidebar update icon should start the update directly

- **Spec:** [docs/product-specs/548-sidebar-update-icon-starts-the-update-directly.md](../../product-specs/548-sidebar-update-icon-starts-the-update-directly.md)
- **Issue:** #548
- **PR:** #547
- **Branch:** faint-lark
- **Status:** completed

## Summary

Backfilled after implementation: the work was done and review-converged before entering the pipeline. Icon click with an update pending now re-checks and calls `StartUpdate` directly.

## Research

`src/app/banners.ts` owns the update flow (`manualUpdateCheck`, `applyUpdateInfo`, `onUpdateAction`); `src/components/Sidebar.tsx` renders `#check-updates-btn`; `src/lib/update-state.ts` derives the button action (`start` only when `canApply`).

## Approach

New `updateOrCheck()` in `banners.ts`: when `updatePending`, re-check (the dot can be stale), render the result, and call shared `beginStaging()` if `updateButtonState(info).action === 'start'`; otherwise behave as `manualUpdateCheck`. Sidebar uses it. Menu/palette keep `manualUpdateCheck` so a check never installs.

### Files to change

- `cmd/hivegui/frontend/src/app/banners.ts` — `updateOrCheck`, extract `beginStaging`
- `cmd/hivegui/frontend/src/components/Sidebar.tsx` — click handler, pending aria-label
- `.changesets/update-icon-starts-update.md` — changeset

### Tests

- `test/dom/check-updates-button.test.tsx` — starts directly; no start when `canApply` false; check-only when nothing pending
- `test/e2e/sidebar-header-actions.spec.ts` — new label

## Decision log

- **2026-11-11** — Icon only; menu/palette still just check. Why: "Check for Updates…" must not install.
- **2026-11-11** — Error-stage + available retries on click. Why: update-state already offers Retry there.

## Progress

- **2026-11-11** — Implemented, PR #547 opened.
- **2026-11-11** — Backfilled spec/plan/issue #548 into the pipeline.

## PR convergence ledger

- **2026-11-11 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 0ff0ca4.

## Gate verdict

- **2026-11-11** — verdict: PASS; phase: —; checks: 5 passed / 0 failed / 0 followups; followups: none; one-line: icon click starts update; all criteria evidenced, no non-goal bleed, changeset present.
  - 2026-11-11 dimensions:
    - acceptance — PASS — 3 dom tests in check-updates-button.test.tsx; aria-label + changeset in diff
    - non-goals — PASS — menu/palette still call manualUpdateCheck; ready/Restart flow untouched
    - doc accuracy — PASS — changeset added; remaining old-label hits are historical specs/plans only

## Open questions

None.
