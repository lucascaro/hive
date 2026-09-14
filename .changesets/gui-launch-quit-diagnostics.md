---
type: changed
bump: patch
---

`hivegui.log` now records why the window started and why it closed: each launch
logs its parent process and whether it was opened from inside a Hive session, and
each quit says whether it came from Quit, the close button, or a signal such as
`killall`. Notification clicks and page reloads are logged too. When Hive restarts
itself unexpectedly, the log now points at what did it. Nothing else changes.
