---
type: added
bump: minor
---

Claude sessions that hand work to subagents now show how many are running, as a
small number on the corner of the plan pie in the sidebar. A subagent's tool
calls no longer take over the session's "running" tool or count against the
parent's plan steps, and a subagent still working after the main agent has
finished no longer flips the session back to working. The plan pie is also
easier to read on dark themes: it's now outlined in the session's state colour,
and it stays tucked under the state icon.
