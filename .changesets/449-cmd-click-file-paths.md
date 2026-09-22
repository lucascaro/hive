---
type: added
bump: minor
issue: 449
---

⌘-click a file path in a session to open it. Paths are underlined on
hover only when the file exists, and relative paths resolve against the
session's worktree or project directory. A file the OS would run — an
application, a script, an installer — is revealed in the file manager
rather than launched, since terminal output is not always yours.
⇧⌘-click opens the file in the editor you pick in Settings › Appearance
(VS Code, Cursor, Zed, Sublime Text, a custom command or a macOS app),
jumping to the `:line:col` in the clicked text. Ctrl replaces ⌘ on
Windows and Linux.
