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
- Light presets darken their "running" and "attention" state colours so the
  worktree status lines they now colour clear WCAG AA.
