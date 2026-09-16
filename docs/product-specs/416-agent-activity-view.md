---
issue: null
pr: null
title: "Agent activity view: see the plan and the tools, not just the state"
type: enhancement
complexity: L
priority: P2
stage: RESEARCH
---

# Agent activity view: see the plan and the tools, not just the state

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Design:** [docs/design-docs/agent-activity.md](../design-docs/agent-activity.md)
- **Mocks:** https://claude.ai/artifact/RGuGZahDBRqnNFVysgJZ7V (version 5 — A7 30px, B3, C3)

## Problem

Spec [336](336-session-state-model.md) made the daemon the source of
truth for *whether* a session is working, waiting or gone. It says
nothing about what the agent is working on. To find that out the user
reads the scrollback, and with a dozen sessions that does not scale —
the same argument that moved session state into the daemon, one level
deeper.

The data already arrives and is discarded. `cmd/hived/hook.go`
collapses `PreToolUse` / `PostToolUse` / `PostToolUseFailure` into a
bare `permission_resolved`, dropping `tool_name` and `tool_input` —
and Claude's plan arrives on the same hooks, as calls to its task
tools.

> **Correction (2026-09-16).** This spec first named `TodoWrite` as the
> plan source. Checked against a live Claude Code 2.1.273 session, that
> was wrong on two counts: `TodoWrite` is disabled by default in favour
> of `TaskCreate` / `TaskUpdate` / `TaskList` / `TaskGet`, and on current
> models (Opus 5, Sonnet 5) Claude Code provides **no** task tools at all
> unless the session opts in with `CLAUDE_CODE_ENABLE_TODO_TOOLS=1`
> (code.claude.com/docs/en/tools-reference, "Task tool availability").
> As first written, the plan indicator would never have appeared on the
> default model. The criteria below are corrected.

## Desired behavior

**Two facts per session.** The *plan* (what the agent set out to do and
which item it is on) and the *tool* (what it is running now, what it
just ran, how long it took, whether it failed).

**Three placements, one component.**

- **Sidebar row.** A small outlined pie sits in the cell *under* the
  state icon — 12px, 11px at `tight` density, 10px at `compact`, where
  the row is one line and the pie moves up to row 1. (This supersedes
  the mocks' 30px ring behind the agent code, which Phase 1 dropped.)
  Pie colour is the session state; the filled slice is the fraction of
  plan items done. Both existing lines — name and window title —
  survive untouched, and no row grows. A session with no plan shows no
  pie, exactly as today. Running subagents (`subagents_running` > 0)
  show as a numeral badge on the pie's corner (phase 2); with no plan,
  the badge sits on an empty outline. With neither, nothing renders.
- **Inspector panel.** Toggled by key beside the terminal in
  single-session view, read-only so the terminal keeps keyboard focus.
  Plan steps with their tool calls nested beneath; only the current
  step is expanded by default, the rest collapse to one line carrying a
  disclosure triangle and a tool-count pill. The full timeline scrolls
  in its own section below.
- **Activity grid.** A keybinding swaps every grid tile's terminal for
  the same component at tile size: plan shape as pips, body given to
  the live tool feed.

**Agent coverage.** Claude via the hook tier, Pi via the extension
tier. Pi's tool events come from `tool_execution_start` /
`tool_execution_end`; its plan comes from a `todo` tool the Hive
extension registers via `pi.registerTool()`, **on by default, with a
setting to disable it**. Every heuristic-tier agent — shell, Codex,
Gemini, Aider, custom — shows an explicit "no activity data" empty
state.

**Privacy.** Raw `tool_input` never crosses the socket. The reporter
(`hived hook`, or the Pi extension) derives a short label — a basename,
a command head, a URL host — and sends only the tool name and that
label. The timeline reads `Bash · npm test`, not the command.

**Staleness.** When a tier goes quiet past `agentstate.HookStaleAfter`,
the plan is still in memory and must not read as live: the panel and
tile show its age and the pie desaturates to `--fg-subtle`.

**Retention.** A bounded in-memory ring (~200 tool events) plus the
latest plan snapshot, per session, dying with the daemon like the PTY.
No disk format.

## Success criteria

- `mapHookPayload` emits `tool_start` / `tool_end` with `tool`,
  `target`, `call_id` and `ok`, preserving the working-state effect the
  collapsed `permission_resolved` had. A successful planning call
  additionally emits the plan: `TaskCreate` / `TaskUpdate` a per-task
  `plan_item` merged by ID (the ID is read from `TaskCreate`'s
  `PostToolUse` response — Claude assigns it), `TaskList` a wholesale
  `plan` resync, and `TodoWrite` a wholesale `plan` for configurations
  that still use it. A failed planning call changes no plan.
  Table-driven tests over payloads captured from a live session cover
  every tool shape, a missing or malformed `tool_input`, and oversized
  arguments.
- Hive opts the Claude sessions it starts into the task tools
  (`CLAUDE_CODE_ENABLE_TODO_TOOLS=1`), **on by default with a Settings
  toggle**, applied exactly where Hive's hooks are applied, and never
  overriding a value the user set themselves. An opt-in real-Claude probe
  (`HIVE_PROBE_CLAUDE=1`) fails if Claude Code stops honouring the
  variable.
- No raw `tool_input` object or secret-bearing argument appears in any
  frame the daemon receives; only the derived label (`target`) and plan
  `items` cross — asserted in a test, not by inspection.
- Ring tests: eviction at the cap, a per-step tool tally that stays
  correct after its events are evicted (the tally lives on the plan
  item, not in the ring), plan replacement, cleanup on session close.
- Daemon tests: `ACTIVITY` reaches control clients through the existing
  broadcast; `GET_ACTIVITY` returns the ring; a `ModeSession`
  connection cannot read another session's activity.
- `SessionInfo` carries `plan_done` / `plan_total` / `current_tool`,
  and the sidebar row renders from those alone.
- Pi extension tests (`internal/agent/pi/hive.test.ts`):
  `tool_execution_start` / `tool_execution_end` post the right events;
  `registerTool` produces a plan; the todo tool disabled posts nothing
  and registers nothing.
- Label derivation is separator-agnostic: a Windows path renders as its
  basename in both reporters.
- Durations come from the daemon's clock across the `call_id` pair,
  never from reporter timestamps.
- **Playwright** (Wails mock, run with `CI=1`): the plan pie does not
  change row height or the agent code's type; the panel toggles without
  stealing terminal focus; the grid keybinding swaps tiles and back.
  vitest is CSS-blind and cannot answer the first of these.

### Phase 2 — subagent attribution

- `mapHookPayload` copies `agent_id` / `agent_type` onto tool and plan
  events. `SubagentStart` / `SubagentStop` map to `subagent_start` /
  `subagent_end`. `Stop`'s `background_tasks` becomes `running_agents`,
  which is nil when the key is absent. Tests replay payloads captured
  from a live session.
- A subagent-tagged event never changes the session's state,
  `current_tool`, plan or plan-step tally. Its tool calls are still
  recorded in the ring with `agent_id` / `agent_type`, and pair across
  the parent's turn end.
- `SessionInfo.subagents_running` rises and falls with the lifecycle
  events. A late start after its end does not resurrect a subagent, a
  `Stop` reconciles the count, and it clears on session end and exit.
- A subagent event that races ahead of the parent's `Stop` does not get
  the `Stop` dropped as out of order. A replay of the captured timeline
  with those two stamps inverted proves it.
- The daemon caps `agent_id`, `agent_type` and `running_agents` at its
  trust boundary. `DaemonContract` is bumped for the new kinds.
- **Playwright:** the subagent badge changes no row height at any
  density and is not clipped. The pie stays under the icon when the row
  has no title.

## Non-goals

- Persisting activity across a daemon restart.
- Raw tool arguments anywhere on the wire or in the UI.
- A structured transcript view — the terminal already renders the
  conversation.
- Budget, cost or token accounting. Pi's `tool_result` carries `usage`,
  so the data is reachable, but what to do with it is a separate
  design.
- A plugin API or runtime loading of third-party visualizations. The
  extension point is an in-repo renderer registry: a second
  visualization is a new file, not a refactor.
- Any cross-session view — a swarm control center, a spawn graph,
  roles. That needs the phases in
  [agent-orchestration.md](../design-docs/agent-orchestration.md) to
  have shipped; there is nothing to aggregate until agents spawn and
  direct each other.
