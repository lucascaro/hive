---
issue: 340
title: "Persist minimized projects across restarts"
type: bug
complexity: M
priority: P2
pr: 342
stage: GATE
---

# Persist minimized projects across restarts

- **Issue:** #340
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/340-persist-minimized-projects-across-restarts.md](../exec-plans/active/340-persist-minimized-projects-across-restarts.md)

## Problem

Minimizing a project in the GUI collapses it into the sidebar tray, but the
collapsed state does not survive an app restart — projects come back
expanded.

**Root cause (confirmed on the operator's machine, 2026-09-05).** Every
hivegui process shares one WKWebView localStorage, keyed on the bundle id
`com.wails.hivegui` — *not* on which daemon it is connected to. Users who run
isolated per-worktree daemons (`HIVE_SOCKET` + `HIVE_STATE_DIR`) therefore have
several GUIs writing one `hive.minimizedProjects` key, while each daemon has
its own registry with completely disjoint project UUIDs.

`applyProjectList` prunes the persisted set against the `project:list`
snapshot, so on every boot each instance deletes the other instances' ids as
"projects that no longer exist". Evidence: the persisted set held
`["7b50fc60-…"]`, a project belonging to `/tmp/hive-iso-azure-comet/state`,
while the connected daemon's registry listed eight unrelated UUIDs; one
quit-and-relaunch of the main GUI rewrote the key to `[]`.

`hive.collapsedProjects` has the identical defect — same key family, same
prune.

## Desired behavior

A project the user minimized is still minimized the next time the GUI
starts, for as long as that project still exists.

## Success criteria

- Minimize a project, quit and relaunch the GUI: the project is still in the
  sidebar tray and its sessions are still absent from grid views.
- Restoring a project and restarting leaves it restored.
- A project that no longer exists is dropped from the persisted set rather
  than stranding a tray chip.

## Non-goals

- Fixing this for two GUI windows attached to the *same* daemon. Last writer
  wins there, matching `window.json`'s documented behaviour.

- Persisting individually minimized **sessions** (`appData().minimized`).
  Those are deliberately transient — the terminals are gone after a restart.
- Moving this state out of localStorage into daemon-owned state.

## Notes

Related: spec 336 (session state model) moved other state daemon-side.
