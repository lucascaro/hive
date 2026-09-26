---
type: fixed
bump: patch
issue: 461
pr: 462
---

A window or client that stops reading from the daemon can no longer freeze a
running agent, stall other windows watching the same session, or hang quitting
and restarting the daemon. The daemon now hangs up on a client that falls
behind, and that client reconnects with a fresh view instead of quietly
missing updates. A terminal whose connection is dropped this way reattaches on
its own, without you having to switch sessions and back.
