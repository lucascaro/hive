---
issue: null
pr: 358
type: added
bump: minor
---
- Ideas in the GUI: **⌘I** opens a capture sheet from anywhere — type
  the note, pick idea / bug / feedback, pick the project (the one you
  are working in, prefilled) — and Enter files it and hands focus
  straight back to the terminal. Each project card shows a badge with
  how many ideas are waiting; clicking it, or **⇧⌘I**, opens that
  project's inbox, where a row can be edited in place, marked done
  (the note is kept, it just leaves the inbox) or deleted behind a
  confirm. Ideas filed from a session's shell with `hived idea add`
  show up in the same list, live. Deleting a project now asks before
  it discards the ideas captured for it.
