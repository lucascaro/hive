---
type: changed
bump: patch
issue: 467
---

Plugins: a plugin's broadcast requests now wait (up to two seconds) while any Hive window is behind on the updates already sent to it, so a burst of changes from one or several plugins can no longer make the daemon disconnect a window.
