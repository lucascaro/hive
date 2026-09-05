---
issue: null
pr: 344
type: changed
bump: minor
---
- The menu bar's summary now reads "2 waiting on you" instead of
  "2 need you", and says the same thing the dots beside the sessions
  do.
- Hovering a session's state icon, in the sidebar or on a tile, now
  shows what that session was asked to do, the last thing the agent
  said, and whether the state was reported by the agent or guessed from
  its terminal output.
- Desktop notifications say which kind of answer is wanted: "waiting
  for permission" when an agent is blocked on a yes/no, "waiting for
  input" otherwise.
