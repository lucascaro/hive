# Hive Hook System

## Overview

Hive fires shell hooks at key lifecycle events. Any executable file placed in `~/.config/hive/hooks/` named `on-{event}` will be run when that event fires.

## Hook Directory

```
~/.config/hive/hooks/
├── on-session-create          # executable script
├── on-session-kill
├── on-session-attach
├── on-session-detach
├── on-session-title-changed
├── on-project-create
├── on-project-kill
├── on-team-create
├── on-team-kill
├── on-team-member-add
└── on-team-member-remove
```

Multiple hooks per event: create a `.d/` directory:
```
~/.config/hive/hooks/on-session-create.d/
├── 01-notify.sh
└── 02-log.sh
```

Scripts in `.d/` are run in alphabetical order.

## Events

| Event | When |
|-------|------|
| `session-create` | A new agent session is spawned |
| `session-kill` | A session is terminated |
| `session-attach` | User attaches to a session |
| `session-detach` | User returns from a session to the TUI |
| `session-title-changed` | A session title is changed (user or agent) |
| `project-create` | A new project is created |
| `project-kill` | A project and all its sessions are removed |
| `team-create` | A new agent team is created |
| `team-kill` | A team and all its sessions are removed |
| `team-member-add` | A session is added to a team |
| `team-member-remove` | A session is removed from a team |

## Environment Variables

Every hook receives these environment variables:

| Variable | Description |
|----------|-------------|
| `HIVE_VERSION` | Hive version |
| `HIVE_EVENT` | Event name (e.g. `session-create`) |
| `HIVE_PROJECT_ID` | UUID of the project |
| `HIVE_PROJECT_NAME` | Human-readable project name |
| `HIVE_SESSION_ID` | UUID of the session (if applicable) |
| `HIVE_SESSION_TITLE` | Title of the session |
| `HIVE_TEAM_ID` | UUID of the team (if applicable) |
| `HIVE_TEAM_NAME` | Name of the team |
| `HIVE_TEAM_ROLE` | `orchestrator`, `worker`, or `standalone` |
| `HIVE_AGENT_TYPE` | `claude`, `codex`, `gemini`, etc. |
| `HIVE_AGENT_CMD` | Full command string used to launch the agent |
| `HIVE_TMUX_SESSION` | tmux session name |
| `HIVE_TMUX_WINDOW` | tmux window index |
| `HIVE_WORK_DIR` | Working directory of the session |

## Timeouts

Each hook has a 5-second timeout. Hooks that exceed this are killed. Non-zero exit codes are logged to `~/.config/hive/hive.log` but do not crash Hive.

## Example Hooks

### Notify on new session (macOS)
```bash
#!/bin/bash
# ~/.config/hive/hooks/on-session-create
osascript -e "display notification \"$HIVE_SESSION_TITLE\" with title \"Hive: New $HIVE_AGENT_TYPE session\""
```

### Log all events
```bash
#!/bin/bash
# ~/.config/hive/hooks/on-session-create
echo "$(date -Iseconds) $HIVE_EVENT $HIVE_PROJECT_NAME/$HIVE_SESSION_TITLE [$HIVE_AGENT_TYPE]" >> ~/hive-events.log
```

### Auto-notify when Claude team completes
```bash
#!/bin/bash
# ~/.config/hive/hooks/on-session-title-changed
if [[ "$HIVE_SESSION_TITLE" == *"DONE"* ]]; then
  echo "Team task completed: $HIVE_TEAM_NAME" | say
fi
```
