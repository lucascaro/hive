---
issue: null
title: "GUI: Jump to next session needing attention"
type: enhancement
complexity: S
priority: P2
stage: DONE
shipped: 2026-07-25
---

# GUI: Jump to next session needing attention

- **Issue:** —
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/240-jump-to-next-session-needing-attention.md](../exec-plans/completed/240-jump-to-next-session-needing-attention.md)

## Problem

Hive already knows which sessions want the user's attention: a terminal bell adds the session
id to `state.attention`, which pulses the sidebar row and grid tile and fires a desktop
notification. But there is no way to *reach* a flagged session from the keyboard. With a dozen
sessions across several projects, the user has to visually hunt for the pulsing row and click
it, or step through every session with ⌘↓ until they land on the right one — which also
scatters the session they were working in.

## Desired behavior

A single keystroke jumps straight to the next session flagged for attention, cycling through
them and wrapping. A second keystroke jumps *back* to the session the user was working in
before the **first** jump — so a round of bells can walk them through several flagged sessions
and one keystroke still returns them to the work they actually interrupted. Landing on a
flagged session clears its pulse, exactly as clicking it does today.

- **⌘B** (Ctrl+B off mac) — next session needing attention.
- **⇧⌘B** — jump back to the session held before the first ⌘B.

## Success criteria

- With two sessions flagged, ⌘B from a third session lands on the first flagged one and clears
  its pulse; a second ⌘B lands on the other.
- ⇧⌘B after several ⌘B hops returns to the session the round started from, not one hop back.
- After ⇧⌘B releases the anchor, the next ⌘B anchors wherever the user is then.
- A minimized session that rings its bell is restored by ⌘B and returned to the tray by ⇧⌘B —
  glancing at it because it asked for you does not permanently drag it back into the grid.
- ⌘B with nothing flagged shows a "no sessions need attention" status flash and does not change
  the active session. A stale flag on the session you're already in (which `onSessionDeath`
  sets unconditionally) is cleared rather than left pulsing.
- ⇧⌘B when the anchor session has since been killed shows a status flash and does not crash.
- The jump works from single, grid-project, and grid-all views, and across projects.
- Both bindings appear in the ⌘/ help overlay and the ⇧⌘K command palette.

## Non-goals

- A multi-entry back-stack. One return slot only.
- A "previous session needing attention" (reverse cycle) binding.
- Changing what raises attention — the bell path in `app/events.js` is untouched.
- Any change to the desktop notification behavior.

## Notes

On Linux/Windows this claims **Ctrl+B** from the terminal (tmux prefix, readline
backward-char). The app already claims Ctrl+T/W/S/G the same way, so this follows existing
precedent; if it proves disruptive the binding can be made mac-only or moved to a punctuation
key.

Numbered 240 rather than 218 because no GitHub issue was created and the shared GitHub
issue/PR number space is already at 239 — 218 is an existing PR.
