---
issue: null
pr: null
type: fixed
bump: patch
---
- Restarting Hive no longer risks leaving the daemon unreachable. When
  the old `hived` shut down while its replacement had already taken
  over the socket path, it could delete the replacement's socket —
  inode numbers get reused, so "is this still my socket?" answered yes
  for a file the old daemon never bound. The dying daemon now asks the
  socket instead of the filesystem, and leaves any socket that is still
  being served alone.
