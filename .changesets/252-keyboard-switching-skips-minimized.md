---
issue: null
pr: 293
type: fixed
bump: patch
---
- Keyboard switching now skips what you minimized. ⌘↑ / ⌘↓ step over
  sessions in the tray and sessions whose project is minimized — they no
  longer pull you back into a project you put away, or drop you out of a
  grid view when they do. ⌘[ / ⌘] likewise cycles only projects still in
  the sidebar. A minimized project stays reachable from its tray chip,
  the sidebar, and ⌘K.
