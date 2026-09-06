---
issue: null
type: added
bump: minor
---
- Ideas: capture a note against a project without interrupting what
  you are doing. From inside any Hive session's shell,
  `hived idea add "the grid loses focus"` files one against that
  session's project (`-k bug` or `-k feedback` for the other kinds),
  and `hived idea list` shows what is waiting — `--all` for every
  project. Ideas outlive the session that filed them, so closing it
  loses nothing. Deleting a project deletes its ideas, and Hive now
  refuses that delete while any of them are still open rather than
  discarding captured work silently. The ⌘I capture sheet and the
  in-app inbox follow.
