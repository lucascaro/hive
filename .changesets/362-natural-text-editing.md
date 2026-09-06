---
issue: 362
pr: null
type: added
bump: minor
---
- macOS text editing in a session now covers the full set: ⌥⌦ deletes the
  word after the cursor and ⌘⌦ deletes to the end of the line, mirroring the
  ⌥⌫ and ⌘⌫ that already worked. Both chords previously did nothing at all —
  xterm encoded them as `\x1b[3;3~` / `\x1b[3;9~`, which no shell binds.
  The keyboard-shortcuts overlay (⌘/) and the README now list every
  terminal-level editing key, not just some of them.
