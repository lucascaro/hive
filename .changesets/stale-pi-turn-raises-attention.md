---
type: fixed
bump: patch
issue: 423
---

A Pi session that stops mid-turn without reporting it (for example, a question whose waiting state never arrived) now shows as needing attention about 30 seconds later instead of quietly reading idle. Looking at a Pi session that needs attention for 3 seconds now clears its status; the question stays on screen.
