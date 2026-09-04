---
issue: null
pr: null
type: added
bump: minor
---
- Claude Code sessions now report their own state instead of relying on
  a guess from terminal output alone. Every session Hive spawns gets
  `HIVE_SESSION_ID` / `HIVE_SOCKET` in its environment, and Claude
  sessions get hooks wired through `claude --settings` that call
  `hived hook` on prompt submit, turn end, and permission prompts. A
  waiting-for-permission session now shows exactly that — not a guess
  from "no output for two seconds" — and the sidebar/tile glyph and
  tooltip get a real prompt/summary instead of nothing. Falls back to
  the existing heuristic tier automatically when `claude` predates the
  hooks it needs.
