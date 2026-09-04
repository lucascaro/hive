---
issue: null
pr: 333
type: changed
bump: minor
---
- Sessions that ring the terminal bell are now tracked by the daemon
  rather than by each window on its own. Every window agrees on which
  sessions want you, a window that was closed or reloaded still learns
  what rang while it was away, and focusing a session clears the flag
  everywhere at once.
