---
issue: null
pr: 344
type: changed
bump: minor
---
- The menu bar now counts the sessions actually blocked on you —
  "2 waiting on you" — instead of the ones whose bell you have not
  cleared yet, and the dot beside a session means the same thing. An
  agent that rang once and went back to work no longer counts against
  you.
- Hovering a session's state icon, in the sidebar or on a tile, now
  shows what that session was asked to do, the last thing the agent
  said, and whether the state was reported by the agent or guessed from
  its terminal output.
- Desktop notifications say which kind of answer is wanted: "waiting
  for permission" when an agent is blocked on a yes/no, "waiting for
  input" otherwise.
