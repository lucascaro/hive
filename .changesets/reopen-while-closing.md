---
type: fixed
bump: patch
issue: 566
pr: 568
---
Reopening or unarchiving a session while its close is still finishing is now refused with "still closing; try again" instead of silently losing the reopened session's files and pins.
