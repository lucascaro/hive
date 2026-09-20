---
issue: 436
title: Show an update pip on the Check for updates button instead of auto-showing the update bar
type: enhancement
complexity: S
priority: P3
pr: 437
shipped: 2026-09-18
stage: DONE
---

# Show an update pip on the Check for updates button instead of auto-showing the update bar

- **Issue:** [#436](https://github.com/lucascaro/hive/issues/436)
- **Exec plan:** [docs/exec-plans/completed/436-update-pip-on-check-for-updates-button.md](../exec-plans/completed/436-update-pip-on-check-for-updates-button.md)

## Problem

When the background update check finds a new version, Hive pops the update banner across the top of the window. That is intrusive for something the user did not ask for. The sidebar's "Check for updates" button should carry an unread-style pip (like the What's New gift's dot) instead, and the banner should appear only when the user asks for it.

## Desired behavior

A background update check that finds a newer version no longer shows the update banner. Instead the sidebar's ⤓ "Check for updates" button shows the same dot the What's New gift uses for unread. Clicking the button runs a check and shows the banner with the version and its Update / Download actions, as it does today. Anything the user started — a manual check from the button, the macOS menu or the palette, and staging, ready and failed updates — still shows the banner. The pip stays until a check reports no update available.

## Success criteria

- A background `update:available` with a newer version leaves the update banner hidden and puts the `hv-unread` dot on `#check-updates-btn`, whose accessible name becomes "Check for updates — update available".
- Clicking the pipped button shows the update banner with the available version and its Update action.
- Staging, ready and error progress still show the banner without being asked.
- The pip persists across later background polls and banner dismissal, and clears once a check reports no update available.
- README's "Updating" section describes the pip instead of an automatic banner.

## Non-goals

- Changing the Settings → Updates section, the Go-side check cadence, or the macOS menu item.
- Changing the What's New pip.
- Any "seen / snooze" state for the update pip.
