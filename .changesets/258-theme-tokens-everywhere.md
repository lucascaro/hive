---
issue: null
pr: null
type: fixed
bump: patch
---
- The worktree browser, the launcher, the command palette, the help overlay and
  the dead-session overlay follow the chosen theme. They carried 31 hard-coded
  colours, so on a light preset the worktree list painted near-black rows on a
  white panel.
- State colours are text now, not just icon fills, so they are held to WCAG AA
  on every ground they are painted on: `hive-light`'s "running", "attention" and
  "error" darken, and `native-dark`'s "error" lightens, so the worktree status
  lines, the merged badge and the destructive action stay readable under every
  preset.
- The merged badge loses its green tint; its text and border carry it.
