---
issue: null
title: "Real end-to-end tests for hive"
type: enhancement
complexity: L
priority: P1
stage: DONE
shipped: 2026-07-25
---

# Real end-to-end tests for hive

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/210-real-e2e-tests.md](../exec-plans/completed/210-real-e2e-tests.md)

## Problem

GUI and daemon regressions kept reaching `main` in a steady drip (#195, #198,
#200/#203, #208/#209). The existing suite drove a JS Wails mock, so nothing
exercised the real wire protocol or real daemon payloads — the exact seam where
those regressions lived.

## Desired behavior

Every PR runs tests against a real `hived` binary over the real wire protocol,
with the frontend talking to a real daemon rather than a mock. Regressions in
the fragile interaction zones fail CI instead of reaching a release.

## Success criteria

- Layer A spawns the real `hived` binary and drives it over the real wire protocol.
- Layer B replaces the JS Wails mock with a bridge to a real daemon.
- Layer C expands the mock-Wails Playwright suite with invariant tests for the
  five fragile interaction zones.
- All three layers run on every PR.
- Runs are isolated: temp `HIVE_SOCKET` and `HIVE_STATE_DIR`, with a
  `testclient.RequireIsolation` guard that fail-closes when either is missing or
  points outside `/tmp`. Production hive state is never touched.

## Non-goals

- Replacing the mock-Wails suite; it stays as Layer C.

## Notes

Spec written retroactively (2026-08-30) from the completed exec plan, which had
no spec behind it. `shipped` is the date the plan was filed into `completed/`.
