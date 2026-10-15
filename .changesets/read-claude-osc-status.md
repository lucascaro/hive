---
type: fixed
bump: patch
issue: 517
---

Pressing Esc to interrupt Claude Code now returns its session to idle right away instead of leaving it "working" until the activity timeout, and a Claude question raises "needs attention" even if its hooks are unavailable. Hive reads the status Claude reports in the terminal (OSC 7501) and answers the query that turns it on.
