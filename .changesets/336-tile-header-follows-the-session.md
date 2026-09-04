---
issue: null
pr: null
type: fixed
bump: patch
---
- A grid tile's header now shows the same thing its sidebar row does.
  Both read the session the daemon broadcasts, where the header
  previously rendered from a copy refreshed only when the grid was
  rebuilt — so a session could disagree with itself depending on where
  you looked, and a tile's window title could stay blank for a session
  whose tile had been rebuilt or was never attached.
