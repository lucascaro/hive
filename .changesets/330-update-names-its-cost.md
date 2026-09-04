---
type: changed
bump: minor
issue: null
---

The update button now says what applying it will cost. A GUI-only
update reads **Reload** and applies without a confirmation prompt,
because it ends nothing; one that replaces the daemon reads **Restart**
and still warns first. Hive tells them apart by asking the staged
build's daemon for its contract before you commit to anything.
