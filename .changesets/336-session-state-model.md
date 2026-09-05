---
issue: null
pr: null
type: added
bump: minor
---
- Every session now shows what it is doing. The sidebar row and the grid
  tile header carry a state glyph — working, idle, waiting for you,
  exited — so a screen of ten agents can be read at a glance instead of
  clicked through one at a time. Hovering names the state in words.
  Hive reads the state from what the terminal actually renders, so it
  works for every agent and for plain shells, and a session that is
  waiting on you keeps saying so until you look at it.
