---
type: fixed
bump: patch
pr: 394
---

Hive no longer flashes console windows across the screen on Windows. Neither
`hivegui.exe` nor the detached `hived.exe` owns a console, so every helper they
ran — the `git` behind worktree inventory and the update check, `gh` for merged
branches, `claude --version`, the daemon probe — was handed a console window of
its own by Windows. It arrived in bursts: a stray popup on every poll, and a
volley when checking for an update. Child processes are now created with no
console window, so the work happens where it always should have, out of sight.
