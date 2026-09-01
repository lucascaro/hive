---
issue: null
pr: null
type: changed
bump: minor
---
- Every dialog — Settings, the worktree browser, the project editor, the help
  overlay and the confirm prompts — is now built on one shell with consistent
  Escape, backdrop and focus behaviour, and one set of form fields.
- New Settings > Appearance: pick a theme preset (System, Hive Dark, Hive
  Light, Classic) and override any design token by hand. Changes apply as you
  make them, reach open terminals, and are remembered.
- Terminal colours now follow the theme: each preset carries its own ANSI
  palette, so Hive Light no longer renders program output in colours tuned
  for a dark background.
