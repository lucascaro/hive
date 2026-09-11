---
type: fixed
bump: patch
issue: null
pr: null
---

Review fixes for the sidebar redesign: sessions sharing a worktree no longer
paint a second colour bar on every row, a shell session's name drops its
repeated "shell" suffix like every other agent's (so two shells on one worktree
are told apart), a grouped session with no window title shows its name instead
of an empty line, the agent glyph stays legible on light themes, and a collapsed
worktree group reports any session inside it that needs you rather than hiding
it.
