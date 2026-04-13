# Feature Backlog

Ordered by priority. Top item is next to work on.
See `features/templates/FEATURE.md` for the feature file template.

## Active

| # | Issue | Title | Stage | Complexity |
|---|-------|-------|-------|------------|
| P2 | #79 | Consolidate hotkey definitions and display between sidebar and grid modes | RESEARCH | M |

## Rejected

| Issue | Title | Reason |
|-------|-------|--------|
| #66 | Hive mode: hexagonal honeycomb grid layout | Tessellation cannot read as a convincing honeycomb on a char grid while preserving usable content area (2026-04-11) |

## Completed

| Issue | Title | PR | Merged |
|-------|-------|----|--------|
| #13 | Fix mouse support | #24 | 2026-04-03 |
| #12 | tmux mouse on by default | #23 | 2026-04-03 |
| #16 | Stale preview on exit back to main view | #20 | 2026-04-04 |
| #22 | Grid mode not restored after attach → detach | #25 | 2026-04-04 |
| #26 | Remove redundant worktree title | #29 | 2026-04-04 |
| #27 | Purple/green full-screen styling | #32 | 2026-04-04 |
| #30 | Terminal flashes previous content on attach/detach | #33 | 2026-04-04 |
| #28 | Grid view previews not updating after detach/reattach | #31 | 2026-04-04 |
| #35 | Selected session should persist across views and attach/detach | #42 | 2026-04-04 |
| #45 | Allow creating new directories in the project dir picker | #47 | 2026-04-05 |
| #46 | Rework dialog system to use a view stack | #50 | 2026-04-05 |
| #48 | Allow creating new sessions from grid view | #48 | 2026-04-05 |
| #49 | Bug: Renaming projects doesn't update the project name | #51 | 2026-04-06 |
| #38 | Session status does not detect "Waiting for input" state | #56 | 2026-04-07 |
| #52 | Focus management: auto-focus on session create and smart fallback on delete | #57 | 2026-04-09 |
| #39 | Remove window list from title bar and add more text colors | #58 | 2026-04-09 |
| #41 | Simplify detach shortcut to a single key combo | #59 | 2026-04-09 |
| #53 | Grid view: arrow keys should wrap between rows | #60 | 2026-04-10 |
| #36 | Add missing tests | #61, #62 | 2026-04-10 |
| #63 | Attach/detach delay can exceed one second | #64 | 2026-04-10 |
| #34 | Terminal bell does not produce audible sound | #65 | 2026-04-11 |
| #55 | Reorder sessions via keyboard | #67 | 2026-04-11 |
| #40 | Ability to reorder sessions within a project | — | 2026-04-11 (dup of #55) |
| #54 | Per-session color for grid cells | #70 | 2026-04-11 |
| #68 | Improve selected session visibility in grid view | #71 | 2026-04-11 |
| #69 | Display changelog since last version on open | — | 2026-04-11 |
| #37 | Code refactor: remove bloat | — | 2026-04-11 |
| #74 | Windows install docs could better highlight WSL path and Go 1.25+ requirement | — | 2026-04-12 |
| #76 | Organize settings into tabbed categories | #81 | 2026-04-12 |
| #80 | Preserve selected project and session when toggling between grid views (g/G) | — | 2026-04-12 |
| #75 | Add custom terminal bell sounds with settings option | #83 | 2026-04-12 |
| #84 | Fix black screen after saving config with no way to continue | #86 | 2026-04-12 |
| #85 | Terminal bell sounds and grid badge during attached sessions | — | — |
| #88 | Fix empty preview when session has insufficient text | — | — |
| #78 | Persist user preferences — Startup View setting | — | — |
