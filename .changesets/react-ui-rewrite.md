---
issue: null
type: changed
bump: patch
---
- Rewrote the desktop GUI's frontend in React 19 with a zustand store, region by
  region, replacing ~13k lines of hand-written DOM bookkeeping. The app looks and
  behaves identically — this is an internal change — but the sidebar, chrome,
  modals and grid now repaint only the parts that actually changed instead of
  rebuilding a whole region on every update. Terminals are untouched: xterm keeps
  its own imperative lifecycle, and no terminal is ever recreated by a re-render.
