---
type: added
bump: minor
issue: 471
---
Plugins can now add to the Hive app itself: a view for a session (a dialog, a panel beside the terminal, a bar above it), commands in the command palette with their own shortcuts, a badge on sidebar rows, and a section in Settings → Plugins. A plugin that breaks — throws, never finishes loading, or floods the app — is stopped on its own and shown as such in Settings with a Disable button, and the rest of the app carries on. The new example plugin, `plugins/session-notes`, pins a note to a session. Plugin API is now 0.2: plugins written for 0.1 must update their `api_version`, and until then Hive shows them as refused.
