---
issue: null
pr: null
type: fixed
bump: patch
---
- Fixed keyboard focus being silently lost in the sidebar and the grid. A
  daemon `session:event` update — one arrives on every phase step, on every
  surviving session after a kill, and whenever the agent-session-id capture
  poll lands, up to 30s after a session starts — rebuilt the whole sidebar,
  destroying whatever the user had focused. `renderGrid` had the same problem
  from re-parenting every tile on every repaint. Session updates now patch the
  existing rows in place, the grid reorders only when the order actually
  moved, and both paths restore focus if a genuine rebuild moves it.
