---
issue: null
pr: 341
type: fixed
bump: patch
---
- A session's state glyph no longer gets stuck showing the wrong thing
  when an agent reports two events at almost the same moment. Each
  report arrives on its own connection, so a fast pair could be applied
  out of order and leave a finished session showing "working" (or a
  resolved prompt still showing "waiting") until the next event. Reports
  that arrive late are now ignored in favour of what the daemon already
  knows. Affects Claude and Pi sessions alike.
