# Features

This file tracks features that are already implemented in Hive. New ideas should go in `FEATURE_REQUESTS.md` until they are built.

## Projects And Sessions

- Organize work into multiple named projects.
- Create multiple persistent sessions per project, each backed by a tmux window.
- Attach to the selected session from the TUI and return later without losing session state.
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
- Show live session previews with ANSI color passthrough and periodic tmux capture refresh.
- Display help and tmux keybinding reference overlays from the main interface.

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
