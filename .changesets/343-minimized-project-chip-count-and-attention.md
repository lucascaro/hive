---
issue: 343
pr: null
type: changed
bump: minor
---
- A minimized project chip now shows how many sessions the project holds
  and how many of them are waiting on you, next to the same state icon the
  sidebar rows and grid tiles use — so a minimized project tells you what
  a collapsed one does, including whether a session is waiting for
  permission rather than just waiting. The collapsed card and the chip now
  derive that count from one shared helper, which also means a session
  that has exited no longer leaves a stale bell behind on either.
