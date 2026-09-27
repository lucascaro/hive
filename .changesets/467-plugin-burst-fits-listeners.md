---
type: changed
bump: patch
issue: 467
---

Plugins: a plugin's rate-budget burst is now 300 (was 2000), and a plugin relaunching Hive's windows (`CLIENT_COMMAND`) costs 100 instead of 10, so a burst of changes from one plugin can no longer make the daemon disconnect Hive's windows.
