---
type: added
bump: minor
issue: 431
---

Find text in a session with ⌘F (Ctrl+Shift+F on Windows and Linux). On a regular
shell it searches the terminal's scrollback and highlights matches in place. On a
full-screen agent like Claude or Pi — where the terminal keeps no scrollback and
holds a single screenful — it searches that session's transcript on disk instead,
so it finds output that scrolled away long ago. Hive picks the right source on its
own; sessions with no readable history say so plainly.
