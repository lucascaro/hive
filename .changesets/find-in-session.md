---
type: added
bump: minor
issue: 431
pr: 432
---

Find text in a session with ⌘F (Ctrl+Shift+F on Windows and Linux). On a regular
shell it searches the terminal's scrollback and highlights matches in place. On a
full-screen agent like Claude or Pi — where the terminal keeps no scrollback and
holds a single screenful — it searches that session's transcript on disk instead,
so it finds output that scrolled away long ago. Hive picks the right source on its
own; sessions with no readable history say so plainly. Search runs from the bottom
up — the first match is the most recent, and Enter steps back through older ones —
and results update live while the box is open, without losing your place.
The transcript reads like the agent session it came from: your prompts stand out,
each tool's output sits under the tool's name with long output collapsed, and it
opens on the most recent output with older history loading as you scroll up.
