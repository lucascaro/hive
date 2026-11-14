---
issue: 551
title: Add Quit, Restart and Start-at-Login controls to the menu bar
type: enhancement
complexity: M
priority: P2
pr: 552
stage: REVIEW
---

# Add Quit, Restart and Start-at-Login controls to the menu bar

## Problem

The only way out of hivebar's menu is "Quit Hive", which also kills the daemon and every running agent. A user who just wants the menu-bar process stopped or restarted (stuck, stale, or unwanted) can't do either without destroying their sessions. Login autostart can only be changed from the GUI's Settings, which is unreachable from the thing you're trying to control.

## Desired behavior

The menu gains "Quit Menu Bar" (exits hivebar only, no confirm; daemon + sessions untouched), "Restart Menu Bar" (relaunches hivebar, daemon untouched), and a checkmark "Start at Login" item that reflects and flips the same login-item registration as Settings → Start at login. Quit lasts until the next hived/hivegui launch respawns it (that is the restart path); only login autostart is user-controlled.

## Success criteria

1. Click Quit Menu Bar: icon disappears, hived still running, every session still attached in the GUI, no confirm dialog.
2. Click Restart Menu Bar: icon disappears and returns, hived PID unchanged, session list repopulates.
3. Start at Login shows a checkmark iff the login item is enabled; clicking toggles it and Settings → Start at login reflects the new state (and vice versa) without relaunch.
4. Toggle failure (e.g. unsigned build) surfaces the reason, as Settings does, and leaves the checkmark truthful.
5. "Quit Hive" and "Restart Daemon…" behave as before (still confirm, still kill sessions).
6. Menu stays a fixed pool (no items added/removed after Ready); new items placed so existing order is preserved.
7. Unit tests for model/menu wiring; README + changeset updated.

## Non-goals

- A persistent "quit sticks" flag or suppressing hived/hivegui respawn (`HIVE_NO_MENUBAR` stays the hard off).
- Changing Quit Hive semantics.
- Windows/Linux.
- New Settings UI (the existing toggle is the re-enable path).
- Dismissing GUI-side behavior.

## Notes

Stated motivation: "I need to stop or restart the bar and I can't."

Open question for the plan: SMAppService registration must be called from the hivegui bundle (`cmd/hivebar/README.md`), so hivebar toggling it may have to route via the GUI/daemon — the plan decides how.
