---
issue: null
pr: null
type: added
bump: minor
---
- Pi sessions now report their own state, the way Claude Code sessions
  already do. Hive ships a small Pi extension with the daemon and loads
  it into every Pi session it spawns, so the sidebar and tile glyphs
  show what Pi is actually doing — working on your prompt, waiting on a
  confirmation, or settled after a reply — with the last thing Pi said
  in the tooltip. Nothing to install: the extension is written to
  Hive's state directory at startup and is inert if you run `pi`
  yourself. Falls back to the existing heuristic tier if it cannot be
  written.
