---
issue: 548
title: Sidebar update icon should start the update directly
type: enhancement
complexity: S
priority: P3
pr: 547
shipped: 2026-11-11
stage: DONE
---

# Sidebar update icon should start the update directly

- **Issue:** #548
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Exec plan:** [docs/exec-plans/completed/548-sidebar-update-icon-starts-the-update-directly.md](../exec-plans/completed/548-sidebar-update-icon-starts-the-update-directly.md)

## Problem

Clicking the sidebar update icon while an update is pending only raised a banner holding a second Update button, so installing took two clicks on two different controls.

## Desired behavior

With an update pending, clicking the icon re-checks and starts staging immediately. Menu and palette "Check for Updates…" still only check.

## Success criteria

- Clicking the icon with an update pending calls `StartUpdate` once (dom test).
- When the build cannot apply updates (`canApply` false), nothing starts and the banner with the Download link shows (dom test).
- With no update pending, a click only runs a check and never calls `StartUpdate` (dom test).
- The icon's accessible name says an update is available and clicking installs it.
- A changeset describes the change.

## Non-goals

- Applying a staged update (Reload/Restart) — keeps its confirm flow via the banner.
- Changing the menu or palette check-for-updates behaviour.
- A "Checking…" banner during the pending-path re-check.
