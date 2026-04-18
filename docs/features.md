# Features

This file tracks features that are already implemented in Hive. For the feature pipeline (requests, backlog, work-in-progress), see `features/BACKLOG.md`.

## Projects And Sessions

- Organize work into multiple named projects.
- Create multiple persistent sessions per project, each backed by a tmux window.
- Attach to the selected session from the TUI and return without losing session state.
  When using the tmux backend (≥ 3.2) the session opens as a floating popup overlay
  so you never leave the TUI; on older tmux the terminal is taken over full-screen.
  Either way the TUI resumes in place after you detach — no restart required.
- Create new projects and sessions directly from the keyboard-driven interface.
- Kill individual sessions or quit the app while keeping tmux sessions running.

## Agents And Teams

- Launch sessions with built-in agent profiles for Claude, Codex, Gemini, GitHub Copilot CLI, Aider, OpenCode, and custom commands.
- Create agent teams with an orchestrator plus workers from the team builder flow.
- Show team hierarchy in the sidebar with role markers for orchestrators and workers.
- Support mixed-agent teams, including different agent types within the same team.
- Aggregate team status from member session status.

## Navigation And Views

- Navigate with vim-style keys, arrow keys, project jump shortcuts, and sidebar expand/collapse controls.
- Filter sessions by name and jump across projects with numbered shortcuts.
- Toggle focus between the sidebar and the preview pane.
- Open a grid view for the current project or for all projects.
- Show live session previews with ANSI color passthrough and periodic refresh; previews always show the most-recent output (bottom of scrollback).
- Display help and tmux keybinding reference overlays from the main interface.
- Full mouse support: left-click sidebar items to select sessions or toggle project/team collapse; left-click the preview pane to focus it and activate the session; left-click a grid cell to move the cursor; scroll wheel scrolls the sidebar, preview, and grid.
- Grid cells show project name, session name, and agent type in each tile so sessions can be identified without consulting the sidebar.
- Quick-reply: press 1–9 on any focused grid cell to send that digit to the session instantly, without attaching or entering input mode.

## Notifications

- Terminal bell from a background session is forwarded so attention-wanted
  agents surface audibly without the user needing to look at the sidebar.
  A 500 ms debounce prevents chatty agents from flooding the output.
- Bell sound is configurable in Settings → General → Bell Sound. Options:
  `normal` (the terminal's default `\a` bell), `bee`, `chime`, `ping`,
  `knock`, and `silent`. Custom sounds play via the platform's audio
  tool (`afplay` / `paplay` / `aplay` on macOS/Linux, PowerShell
  `SoundPlayer` on Windows) and fall back to `\a` if no player is
  available. Silent mode keeps the sidebar `♪` indicator but makes no
  noise.

## Titles, Status, And Hooks

- Rename sessions and teams inline from the TUI.
- Update session titles from agent output via standard OSC 2 sequences or the Hive title marker.
- Preserve user-set titles by default unless configuration changes that precedence.
- Fire shell hooks for project, session, team, and title lifecycle events.
- Expose rich `HIVE_*` environment variables to hook scripts.

## Configuration And Customization

- Create a default config in `~/.config/hive/` on first launch.
- Configure agent commands through the `agents` map, including custom agent entries.
- Customize keybindings through the config file.
- Configure team defaults such as orchestrator type, worker count, and default worker agent.
- Configure preview refresh behavior, hook settings, and title precedence behavior.
- The in-app Settings screen (`S`) is organized into tabs — General, Team Defaults, Hooks, Keybindings. Switch tabs with `←`/`→` or `h`/`l`; `j`/`k` navigates fields within a tab.
