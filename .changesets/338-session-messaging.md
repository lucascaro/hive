---
type: added
bump: minor
pr: 515
---

Message a session. ⇧⌘M hands the active session a note — typed into its terminal when it is quiet, or queued for when it next settles if it is working (an ACP session takes it as a turn, and refuses it while busy). A Claude Code session receives it through Claude's own inbox, working or not, without anything being typed. ⌥⌘B, or the bell on a session's row, notifies you once when the session stops working. `hived msg <session> <text…>` does the same from a shell.
