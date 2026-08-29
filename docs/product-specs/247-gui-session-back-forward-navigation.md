# GUI: back / forward navigation between sessions

- **Issue:** —
- **PR:** #248
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/247-gui-session-back-forward-navigation.md](../exec-plans/completed/247-gui-session-back-forward-navigation.md)

## Problem

The GUI has more than ten ways to change the active session — sidebar click, tile click, ⌘1–9, ⌘arrows, ⌘[/], ⌘B attention jump, minimized-tray restore, plus programmatic switches when a session is created, removed, or rings its bell from an OS notification. None of them leaves a trail. Once you have jumped somewhere, the only way back to the session you were working in is to remember which one it was and find it again in the sidebar. ⇧⌘B exists but is scoped to the attention-jump round (`src/app/keyboard.js:376`): it returns you to the anchor held before the *first* ⌘B and does nothing at all after an ordinary click.

## Desired behavior

The app keeps a back/forward history of visited sessions, exactly like an editor or a browser. However you got to the session you are looking at — clicking it, a keyboard shortcut, or the app switching you there on its own — one keypress takes you back to the one before it, and another takes you forward again.

The chord matches VS Code, per platform: **Ctrl+-** back and **Ctrl+Shift+-** forward on macOS; **Ctrl+Alt+-** and **Ctrl+Alt+Shift+-** on Windows and Linux, where plain Ctrl+- is already the app's zoom-out. History is a stack: navigating somewhere new after going back discards the forward branch. It lives only for the life of the window, and sessions that get killed are skipped rather than dead-ending the walk.

## Success criteria

- Visiting sessions A → B → C and pressing back twice lands on B, then A; pressing forward returns to B.
- The history records switches made by **every** path, including clicking a tile directly in grid view — a path that bypasses `switchTo` and writes `state.activeId` through `setActive` only.
- Pressing back/forward does not itself add history entries (no ping-pong between two sessions).
- Going back from a session in one project to a session in another switches the sidebar selection, window title, and current project along with it.
- ⌘= / ⌘- / ⌘0 still zoom on macOS, and plain Ctrl+- still zooms on Windows and Linux.
- Going back into a session you had minimized restores it to the grid and puts the keyboard in it, rather than selecting an invisible tile.
- Killing the session at the top of the back stack makes back skip it instead of erroring or doing nothing.
- With no history, back reports "nothing to go back to" in the status bar rather than failing silently.

## Non-goals

- Persisting history across app restarts.
- History entries for project switches that land on an empty project (no active session).
- A visible back/forward UI affordance (buttons, breadcrumbs) — keyboard and command palette only.
- Making the binding user-configurable; the GUI has no keybinding config today.

## Notes

VS Code uses `Ctrl+-` / `Ctrl+Shift+-` for Go Back / Go Forward on macOS and `Ctrl+Alt+-` on Windows/Linux for the same reason this spec does: `Ctrl+-` is zoom on those platforms.

Accepted cost: on macOS `Ctrl+-` currently falls through to the PTY, where `Ctrl+_` is readline/emacs undo. The app's capture-phase keydown listener will now swallow it.
