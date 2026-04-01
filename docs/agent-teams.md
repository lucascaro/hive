# Agent Teams in Hive

## Overview

An **agent team** is a coordinated group of AI agents working on the same goal. One agent serves as the **orchestrator** (directing work) and any number of **workers** assist with subtasks.

Teams are shown in the sidebar with a three-level hierarchy:
```
▼ [team] feature-x ◉
  ◉ ★ orchestrator [claude]
  ○ worker-1     [claude]
  ○ worker-2     [codex]
```

Each session row keeps the status inline as a colored dot:
- `○` gray: idle
- `●` green: working
- `◉` amber: waiting
- `✕` red: dead

Hive shows a status legend in the main status bar and in the grid view footer so the color meanings are always visible.

## Creating a Team

Press `T` in the sidebar to open the team wizard:

1. **Team name** — display name for the team
2. **Team goal** — optional description of what the team is working on
3. **Orchestrator** — select the agent type for the orchestrator (Claude recommended)
4. **Worker count** — number of worker agents (1–10)
5. **Working directory** — shared directory where all agents work
6. **Confirm** — creates all tmux windows and launches all agents

## Claude Agent Teams

Claude natively supports multi-agent orchestration. A typical Claude team setup:

1. The **orchestrator** Claude receives the high-level task
2. The orchestrator breaks the task into subtasks and assigns them to workers
3. Each **worker** Claude handles a focused subtask
4. Workers report back to the orchestrator via shared files or direct conversation

### How to use in Hive

1. Create a team with orchestrator: `claude`, workers: 2x `claude`
2. Attach to the orchestrator (`a`): describe the overall task
3. Claude's orchestrator will spawn subagents or direct workers
4. Switch between worker sessions to monitor progress
5. Workers can signal completion via title: `\033]2;DONE: auth module\007`

## Team Status Aggregation

Team status in the sidebar is derived from member statuses:
- If any member is `waiting` → team shows as `waiting`
- If any member is `running` → team shows as `working`
- If all members are `idle` → team shows as `idle`
- If all members are `dead` → team shows as `dead`

## Mixed-Agent Teams

Teams can combine different agent types:
```
▼ [team] full-stack
  ★ orchestrator [claude]   — planning + coordination
  ○ worker-1     [codex]    — TypeScript implementation
  ○ worker-2     [aider]    — refactoring + cleanup
  ○ worker-3     [gemini]   — documentation
```

## Team Hooks

Teams fire additional hook events:
- `team-create` — when the team is first created
- `team-kill` — when the team is destroyed
- `team-member-add` — when a session is added to the team
- `team-member-remove` — when a session is removed from the team

See [hooks.md](hooks.md) for environment variables available in team hooks.

## Title Signaling

Agents can signal status via the OSC 2 escape sequence (standard xterm window title):
```
printf '\033]2;DONE: feature complete\007'
```
Or via the Hive-specific null-byte marker:
```
printf '\000HIVE_TITLE:feature complete\000'
```

This updates the session title in the sidebar and fires the `session-title-changed` hook.
