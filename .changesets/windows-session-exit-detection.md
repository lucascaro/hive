---
type: fixed
bump: patch
---

Sessions on Windows now notice when their process exits. When an agent finished,
a user typed `exit`, or a tool crashed, the tile stayed alive forever: nothing in
Hive waited on the child, and the session only ever learned about an exit from a
PTY read error — which never came, because conhost keeps the ConPTY output pipe
open until Hive closes the pseudoconsole. The read stayed parked for minutes, so
the session never went to `exited`, its post-spawn session-id capture kept
polling, and the reader goroutine, its OS thread, the conhost process and the
pseudoconsole all leaked for the life of the daemon. Hive now waits on the
process itself and releases the PTY once the child is gone, after a short grace
period so the last thing the agent printed still lands on the tile.
