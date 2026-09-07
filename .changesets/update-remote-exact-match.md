---
issue: null
pr: 365
type: fixed
bump: patch
---
- The "latest" update channel now checks that the source checkout's
  remote is exactly github.com/lucascaro/hive before pulling and
  building from it. A URL that merely contained that text used to pass.
