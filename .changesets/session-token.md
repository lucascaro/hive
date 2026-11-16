---
type: security
bump: patch
---

A program running inside one Hive session can no longer act as, or report state for, a different session: each session now proves its identity to the daemon with a secret only its own processes receive.
