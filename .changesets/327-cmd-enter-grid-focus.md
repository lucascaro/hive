---
issue: 327
pr: 328
type: added
bump: minor
---
- ⌘⏎ (Ctrl+Enter off macOS) in a grid view now focuses the active session,
  switching to single view on the tile you navigated to. The binding is
  deliberately one-way — single → grid stays on ⌘G / ⇧⌘G — so that in single
  view the key falls through to the terminal, where Claude and Codex bind
  Cmd+Enter themselves.
