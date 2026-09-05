---
issue: 343
pr: 346
type: changed
bump: minor
---
- A minimized project chip now shows how many sessions the project holds
  and how many of them are waiting on you, next to the same state icon the
  sidebar rows and grid tiles use — so a minimized project tells you what
  a collapsed one does, including whether a session is waiting for
  permission rather than just waiting.
- A minimized chip and a collapsed project card now derive "needs you"
  from one shared rule, so the two can no longer disagree. That rule is
  stricter than the old one on both: a session that has exited, or that
  has not finished starting, no longer counts as waiting on you, so a
  stale bell can't outlive the session that raised it.
