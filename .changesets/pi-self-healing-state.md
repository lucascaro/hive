---
type: fixed
bump: patch
issue: 423
pr: 424
---

A Pi session no longer loses its "needs attention" state when a state report from Pi goes missing: Pi now re-sends its latest state every few seconds, so a question shows as waiting within about 5 seconds even if the first report was lost. A live Pi session also stays on reported state instead of falling back to guessing from terminal output, so the state tooltip reads "reported by the agent" more often. Updating restarts the daemon.
