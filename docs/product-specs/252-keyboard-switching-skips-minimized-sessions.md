# Keyboard switching skips minimized sessions and projects

- **Issue:** —
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/252-keyboard-switching-skips-minimized-sessions.md](../exec-plans/active/252-keyboard-switching-skips-minimized-sessions.md)

## Problem

Since #250 / #290 shipped project minimizing, a user can put a whole project into
the sidebar tray to get it out of the way. Its sessions stop tiling in grid
views — but the keyboard still walks straight through them. `⌘↓` steps onto a
session inside a minimized project, and because that session has no tile, the
grid silently falls back to single mode. The same holds for `⌘[` / `⌘]`, which
cycles minimized projects as if they were still in play.

The result is that minimizing does not actually take a project out of the way:
every cycle through the sessions drops you back into the thing you put away, and
in a grid view it costs you your grid.

## Desired behavior

Minimized things are out of the keyboard rotation. `⌘↑` / `⌘↓` step over any
session that has no tile — whether it was minimized on its own or its project
was — and `⌘[` / `⌘]` cycles only projects still in the main sidebar list. When
everything else is hidden, the keys do nothing rather than moving you into the
tray.

A minimized project stays reachable the way you put it away: its tray chip, the
sidebar, and `⌘K`.

## Success criteria

- With a project minimized, holding `⌘↓` through a full cycle never selects one
  of its sessions, and never drops out of a grid view.
- `⌘↑` / `⌘↓` also skip individually-minimized sessions (the session tray).
- `⌘]` / `⌘[` move to the next/previous project that is **not** minimized.
- When every other session is hidden, `⌘↓` leaves the active session unchanged;
  when every other project is minimized, `⌘]` leaves the current project
  unchanged.
- `⇧⌘↑` / `⇧⌘↓` still reorder across a minimized sibling exactly as before —
  reordering manipulates order, not focus.
- Restoring the project puts it back in both rotations with no further action.

## Non-goals

- `⌘1-9` is unchanged. It is a positional index into the full ordered session
  list; filtering it would renumber sessions every time something is minimized.
- No change to the sidebar, tray, or `⌘K` palette — all three keep listing
  minimized things, which is how you get them back.
- No new keybinding for minimize/restore.

## Notes

Supersedes part of [#250](250-minimize-projects-from-sidebar.md), whose success
criteria listed `⌘[ / ⌘]` among the ways a minimized project stays reachable.
That criterion is amended by this spec.

The fix reuses `isSessionHidden()` in `cmd/hivegui/frontend/src/app/view.ts`,
which already answers "is this session out of the grid, by either mechanism?".
