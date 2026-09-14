---
type: fixed
bump: patch
issue: 407
pr: 408
---

Minimized sessions no longer stay in the sidebar as dimmed rows that ⌘↑/⌘↓ skip over. Minimizing a session now takes it out of the sidebar, the same way minimizing a project does, so every row you see can be reached from the keyboard. Get it back from the tray above the status bar or with ⌘K. If the session you are on is minimized, its row stays until you move to another session.

⇧⌘↑ / ⇧⌘↓ also skip minimized sessions now. The active session moves past the next visible session instead of swapping places with one you can't see, and the minimized session keeps its place in the order.
