---
issue: null
pr: 307
type: fixed
bump: patch
---
- Fixed the `e2e-real` test harness, which had been failing on `main` for
  reasons unrelated to any diff. `hived-ws-bridge` dispatched every JSON-RPC
  frame on its own goroutine, so under CPU contention adjacent `WriteStdin`
  keystrokes reached the pty out of order and the commands the specs typed
  were not the commands the shell ran. `WriteStdin` is now applied in arrival
  order. Test-only: the shipped GUI does not use this bridge.
