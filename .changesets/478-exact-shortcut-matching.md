---
type: changed
bump: patch
issue: 478
pr: 480
---
Keyboard shortcuts now fire only on the exact keys they show. Adding ⌥/Alt to a ⌘/Ctrl shortcut no longer triggers it: on Windows and Linux, Ctrl+Alt+T no longer acts as Ctrl+T, so AltGr characters type normally. The shortcuts defined with Ctrl+Alt (session back/forward, the agent-activity grid) still work. ⇧⌘E and ⇧⌘S no longer act as ⌘E (worktrees) and ⌘S (sidebar). The keyboard-shortcuts overlay and Help modal close only with ⌘/ (Ctrl+/ on Windows/Linux) or Esc. "Capture Idea…", "Ideas…" and "Keyboard Shortcuts" in the command palette now toggle exactly as their shortcuts and menu items do.
