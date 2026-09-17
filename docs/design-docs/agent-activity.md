# Agent activity

How Hive learns *what an agent is doing inside a turn* — which tool it
is running and where it is in its own plan — and why that answer is
collected in the daemon, derived in the reporters, and rendered from
one component in three places.

This is the observability layer that [control-plane.md](control-plane.md)
stops short of. The control plane answers "is this session working,
waiting, or gone". This doc answers "working on **what**". It changes
nothing in the control plane's state model; the IPC it adds is the
`GET_ACTIVITY` request and `ACTIVITY` broadcast described under [Wire](#wire).

Implemented by [416 — agent activity view](../product-specs/416-agent-activity-view.md).

## The question it answers

A session in `working` state is a black box with a terminal in front of
it. To know what it is doing you read its scrollback, and with a dozen
sessions that does not scale — which is the same argument that put
session state in the daemon, applied one level deeper.

Two facts answer most of it:

- **The plan.** What the agent set out to do, and which item it is on.
- **The tool.** What it is running right now, and what it just ran.

Both already exist in data Hive receives and discards.

## Where the data comes from

The hook and extension tiers already carry it; Hive was throwing it
away.

**Claude (hook tier).** `cmd/hived/hook.go` wires `PreToolUse`,
`PostToolUse` and `PostToolUseFailure`. Before this feature it collapsed
all three to `AgentEventPermissionResolved` and dropped the payload's
`tool_name` and `tool_input`; they now report `tool_start` / `tool_end`
with the tool and a derived label. These invocations already happened on
every tool call of every Hive Claude session, so no new process is
spawned. Phase 2 added `SubagentStart` and `SubagentStop` (below).

Claude's plan arrives on the same hooks, as calls to its **task tools**.
Every shape below was captured from a live Claude Code 2.1.273 session —
an earlier revision of this doc named `TodoWrite`, and was wrong:

| Tool | Payload (on a successful `PostToolUse`) | Becomes |
|------|------------------------------------------|---------|
| `TaskCreate` | input `{subject, description}`; the task's ID is assigned by Claude and appears **only** in `tool_response.task.id` | `plan_item` (insert) |
| `TaskUpdate` | input `{taskId, status?, subject?}` — only what changed; `status: "deleted"` removes it | `plan_item` (merge by ID) |
| `TaskList` | `tool_response.tasks` is the **complete** list | `plan` (wholesale resync) |
| `TodoWrite` | `tool_input.todos` is the whole list | `plan` (wholesale) |

The task tools are incremental — one task per call — and `hived hook`
is a new process per event with no memory of the list, so the daemon
assembles the plan. `TaskList` is a free resync that heals any update
the daemon missed. A failed planning call changes no plan.

**The opt-in.** `TodoWrite` is disabled by default in favour of the
task tools, and on current models (Opus 5, Sonnet 5) Claude Code
provides no task tools at all unless the session sets
`CLAUDE_CODE_ENABLE_TODO_TOOLS=1`. Without it Hive receives no plan. So
Hive sets it for the Claude sessions it starts — **on by default, with a
Settings toggle**, the same rule this doc already applies to Pi's
Hive-registered todo tool. It is applied exactly where Hive's hooks are
(the two share one gate, `claudeHooksAvailable`, since the tools cost
context in every session and only pay off through the hooks), read at
spawn so a toggle reaches the next session without a daemon restart,
and never set when the user's own environment already sets it. The
setting lives in `agent-settings.json` beside `agents.json`: written by
the GUI, read by hived, no wire change.

If Claude Code ever drops the variable nothing errors — it is silently
ignored and the plan indicator simply never appears. The opt-in probe
`TestClaudeProbeTaskToolsOptIn` (`HIVE_PROBE_CLAUDE=1`) is how that
shows up on upgrade rather than in a user's sidebar; it was confirmed to
fail with the setting off.

**Subagents (phase 2).** Claude fires the same tool hooks for calls
made *inside* subagents, on the parent's `session_id`, adding `agent_id`
and `agent_type`, which are present only there. Captured from Claude Code
2.1.273 (fixtures in `cmd/hived/testdata/hooks/subagent/`):

- The parent's `Stop` fires while subagents are still running, and their
  tool events keep arriving after it.
- `Stop` carries `background_tasks`, which lists the subagents still
  running.
- A general-purpose subagent has no task tools, so it cannot touch the
  parent's plan.

Hive tags those events and applies one rule: **an event with `agent_id`
is recorded but never moves the session's state, `current_tool`, plan or
plan-step tally.**
- Subagent calls stay in the ring, tagged, for the panel to nest.
- `SubagentStart` / `SubagentStop` become `subagent_start` /
  `subagent_end`, and `SessionInfo.subagents_running` counts them.
- The parent's `Stop` reconciles the count against `background_tasks`, so
  a lost `SubagentStop` cannot pin it.

The ordering guard compares main-thread events only. Subagent hooks race
the parent's, and one landing first would otherwise get the parent's
`Stop` dropped as out of order.

**Pi (extension tier) — phase 3.** Verified against
`@earendil-works/pi-coding-agent` 0.85.1 (`dist/core/extensions/types.d.ts`):

| Event | Payload | Use |
|-------|---------|-----|
| `tool_execution_start` | `toolCallId`, `toolName`, `args` | → `tool_start` |
| `tool_execution_end` | `toolCallId`, `toolName`, `result`, `isError` | → `tool_end` |

Pi also exposes `tool_call` / `tool_result`, which are **blocking**
handlers that can mutate or veto the agent's arguments. Hive's
extension is an observer and must not sit in that path: a wedged
socket write there would stall the agent's own tool execution. Use the
non-blocking `tool_execution_*` pair.

Pi has **no built-in todo tool** — `dist/core/tools/` is bash, edit,
read, write, grep, find, ls, powershell. The plan comes instead from a
`hive_todo` tool the Hive extension registers through `pi.registerTool()`
(not `todo`: Pi's own example extension already uses that name), on by
default and disableable in settings. This is Hive adding a tool to the
user's agent, so it is a setting, not a constant: `pi_todo_tool` in
`agent-settings.json`, passed to each Pi session Hive starts as
`HIVE_PI_TODO_TOOL=1` or `=0`. Explicit both ways, because it is Hive's
own variable and a value the daemon inherited must not override the
setting. Off, the extension registers nothing and reports no plan.

The tool takes the whole list on every call, so it reports a wholesale
`plan`, together with the call's `tool_end` on one connection. The
plan is read from `tool_execution_end`'s result details, so a failed
call changes no plan. Those details are also where the list lives in
the session, as in Pi's own todo example: on every `session_start`
(startup, reload, `/new`, `/resume`, fork) and on a `/tree` jump the
extension reports the plan of the branch it landed on — the last
successful `hive_todo` result, or an empty plan — so a resumed session
shows its plan straight away and a new one never shows the old one's.

The extension sends its reports one at a time: the next connection is
dialed only once the previous one closed. The daemon drops a wholesale
`plan` stamped older than an event it already applied, and with a
connection per report a parallel tool's `tool_end` could overtake the
last plan update and erase it. The queue is bounded at 64 and drops its
oldest report when full; behind a wedged daemon the backlog is stale
anyway.

Its state reports also heal themselves (spec 423). Every state-bearing
event (`prompt`, `permission_resolved`, `tool_start`, `tool_end`,
`turn_end`, `idle`, `error` and the two waits) carries an ordering key:
`instance`, minted each time Pi loads the extension (startup, `/new`,
`/resume`, fork, `/reload`), and `seq`, counting that instance's state
events. Every 5 s the extension re-sends its latest state event
byte-for-byte, but only when nothing is queued and only when the
spawning daemon set `HIVE_PI_HEARTBEAT=1`. The daemon applies a key it
has not seen, which is how a lost report heals, and treats a seen one
as proof of life. So a wait the user cleared stays cleared, and a live
Pi never goes stale and falls back to guessing from the terminal. If
the heuristic tier took the session during a stall, the next beat
restores the extension's last state, including a clear. `plan`, `ping`
and `session_end` stay unkeyed and keep the timestamp ordering: a
delivered plan must never make a lost state report look already
applied. Only the latest state heals; a lost tool event from the middle
of a batch leaves a gap in the timeline.

**Everyone else (heuristic tier).** Shell, Codex, Gemini, Aider and
custom agents produce no activity. They get an explicit "no activity
data" empty state, never a blank panel that reads as a bug.

## The privacy rule

`tool_input` routinely contains secrets: full shell command lines,
file contents, URLs with tokens. It never crosses the socket.

The reporter — `hived hook` on the Claude side, the extension on the
Pi side — derives a short **label** from the arguments (a basename, a
command head, a URL host) and sends that. The daemon receives a tool
name and a label, never raw arguments, so there is nothing to strip
and nothing to forget to strip. The cost is that the timeline says
`Bash · npm test`, not the exact command.

Enforcing this at the source rather than in the daemon is the whole
point: the ring is readable by every GUI window and by `hivebar`, and
a redaction the daemon performs is a redaction the daemon can regress.

Labels are derived with a separator-agnostic basename on both sides —
never `filepath.Base`, which honours only the compiling platform's
separator. Otherwise the same edit reads `machine.go` on macOS and
`internal\agentstate\machine.go` on Windows. The two reporters share one
table of label cases, `internal/agent/pi/testdata/toollabel_vectors.json`, which
the Go and TypeScript suites both run.

## Wire

Two new `AgentEvent` kinds and a broadcast frame. Nothing else moves.

- **`tool_start` / `tool_end`** carry `tool`, `target` (the derived
  label), `call_id` pairing the two, and `ok` on the end. They keep the
  state effect the collapsed `permission_resolved` had — a running tool
  means *working*.
- **`plan`** carries `items: [{text, status}]`, statuses
  `pending | active | done`.
- **`ACTIVITY`** is broadcast to all control clients through the
  existing pub/sub (`internal/daemon/commands.go`), carrying one delta.
  It is not subscription-gated: the frames are ~100 bytes at a handful
  per second per session, on a connection that already streams raw PTY
  bytes, and the activity grid wants every session anyway. A
  subscription protocol would be real complexity bought with no
  measurable saving.
- **`SessionInfo` gains `plan_done` / `plan_total` / `current_tool`** —
  the compact summary the sidebar row and `hivebar` render. They never
  read the ring, and the summary rides the existing snapshot, so late
  joiners are correct with no new plumbing.
- **`GET_ACTIVITY{session_id}`** returns the stored ring, issued when a
  panel or activity tile first renders. Clients then track the
  broadcast deltas. Nobody pays for 200 events × N sessions at connect.
- **`stale_at`** rides every `ACTIVITY` frame and the `GET_ACTIVITY`
  answer: the daemon-clock instant of the tier's last accepted report
  plus `HookStaleAfter`. So that a client's copy never lags a report,
  every accepted agent event sends a frame — the kinds with no activity
  (prompt, turn end, ping, the waits, subagent lifecycle) send a bare
  liveness frame. An event the ordering guard rejects sends nothing.
  `stale_at` is monotonic per session, which also lets a client order a
  snapshot against deltas: they are written by different daemon
  goroutines and arrive in either order.
- **`plan`** on a frame is `null` when unchanged and `[]` when the plan
  was emptied. It is never omitted-as-empty, which would make the two
  indistinguishable.

Durations are computed by the daemon from its own clock across the
`call_id` pair, never from the two reporter timestamps, which can
straddle a clock adjustment — the rule `AgentEvent.At` already follows.

## State

Per session, in `internal/agentstate` beside the existing machine, with
the same lifecycle (trimmed on session close, so there is no second
place to leak):

- A bounded **ring** of the last N tool events (a constant, ~200).
- The latest **plan** snapshot, replaced wholesale on each `plan` event.

Both die with the daemon, like the PTY. No disk format, no pruning, no
on-disk record of what the agents did.

**The tally lives on the plan item, not in the ring.** A collapsed step
in the panel shows how many tools it ran; once those events age out of
the 200-event ring, a count derived from the ring would be wrong. Each
tool event is stamped with the plan item active when it arrived, and
the item keeps its own counter.

## Rendering

One component, three placements — activity is a *renderer*, not a
screen:

Phase 1 shipped the sidebar row; phase 4 the inspector panel and the
activity grid.

- **Inspector panel** beside the terminal in single-session view,
  toggled by key, read-only so the terminal keeps focus.
- **Activity grid**, a keybinding that swaps every grid tile's terminal
  for the same component at tile size.
- **Sidebar row**, which shows only the `SessionInfo` summary.

The layouts are settled and drawn in the round-five mocks. In short:

- **Sidebar** — a small `conic-gradient` pie inside a 1.5px outline
  (12px; 11px tight, 10px compact) in its own grid cell, top-aligned
  beneath the state icon and coloured by the row's state. At 0% it is an
  empty outline. The running-subagent count sits on the pie's lower-right
  corner as a numeral badge, reading `9+` past nine. With no plan, the
  badge sits on the empty outline. It never overlays or restyles the text, so no row
  grows; both existing lines (name and window title) survive untouched.
  At compact density it moves into row 1 beside the icon, widening
  column 1 only for rows that have a plan.
- **Panel** — plan steps with their tools nested beneath. Only the
  current step is expanded by default; the rest collapse to one line
  with a disclosure triangle and a tool-count pill. The full timeline
  scrolls in its own section below.
- **Grid tile** — plan shape as pips, body given to the live tool feed.

Placement, as built in phase 4:

- **The panel is `#app`'s third column** (`#activity-panel`), not a child
  of `#terms` — whose children `grid-layout.ts` owns — and not inside the
  terminal host, whose `mousedown` selects the session. The terminal
  refits through its own ResizeObserver.
- **The activity grid is an overlay in each tile's `.tile-overlays`**,
  over a terminal body set to `visibility: hidden`. The xterm is never
  unmounted and keeps its size, so leaving the activity grid costs no
  refit, WebGL slot or re-attach. Entering it drops keyboard focus, and
  focus code treats it like a modal, so no keystroke reaches a terminal
  nobody can see.
- **Keys:** ⌘J toggles the panel in single view and the activity grid in
  a grid; ⇧⌘J goes to the activity grid from anywhere (with one session,
  it opens the panel). Off macOS they are Ctrl+Shift+J and
  Ctrl+Alt+Shift+J: plain Ctrl+J is the terminal's newline byte, which
  Claude Code documents as its multiline key.
- **Subagent calls** group by `agent_id` under the plan step. Nothing on
  the wire links a subagent to the parent call that spawned it, so they
  cannot nest under that call.

Every colour is an existing token — `--state-running`,
`--state-attention`, `--state-error`, `--state-info`, `--accent` — so
every theme preset works without a new token.

**Extensibility is an internal seam, nothing more.** One in-repo
registry maps activity events to a React component, so a second
visualization is a new file rather than a refactor. There is no plugin
API and no runtime loading of third-party code: a visualization reads
every session's activity, and loading untrusted JavaScript into that
position would need sandboxing, signing and a cross-platform loader to
buy a capability nobody has asked for yet.

## Staleness

When a tier goes quiet past `agentstate.HookStaleAfter` — agent
upgraded, hook broken, extension not loaded — session state falls back
to the heuristic tier, but the plan is still in memory and would
otherwise render as live. **A stale plan shown as current is worse
than no plan.** Past the staleness threshold the panel and tile show
the plan's age and the ring desaturates to `--fg-subtle`: the same
rule the state machine already applies, extended to activity.

**Only while working.** Neither Claude's hooks nor the Pi extension
heartbeat, so a healthy tier at rest is as silent as a dead one. The
renderers show stale when the session is on the heuristic tier, or when
it is working and `stale_at` has passed. A session at rest never does,
and a hook that dies while the session is at rest cannot be detected —
the same limit the state machine has (`Tick` only demotes working).
`stale_at` is compared with the GUI's clock; on one host that is the
daemon's, and over a remote bridge a skew larger than `HookStaleAfter`
would misreport.

## Cross-platform

The only platform-conditional code is the activity chords (see
Rendering). `internal/daemon/socket.go` uses AF_UNIX on Windows as
well — there is no named-pipe split — and the Claude hooks carrying
this data already fire on both platforms today. The other
platform-specific details are the separator-agnostic label derivation
above and the daemon-side duration clock.

## What is deliberately not here

- **Persistence.** Activity dies with the daemon. Making it survive
  means a file format, size caps, pruning and fsync care on Windows, in
  exchange for history nobody has needed yet.
- **Full tool arguments.** See the privacy rule.
- **A transcript view.** The terminal already renders the conversation;
  a structured mirror of it is a different feature.
- **Budget, cost and token accounting.** Pi's `tool_result` carries
  `usage`, so the data is within reach, but what to *do* with it is a
  separate design.
- **A plugin API.** See the seam above.
- **Cross-session aggregation** — a swarm control center, a graph of
  who spawned whom, roles. That layer needs
  [agent-orchestration.md](agent-orchestration.md)'s phases to have
  shipped first; there is nothing to aggregate until agents spawn and
  direct each other.

## Alternatives considered

- **Derive activity in the GUI** from the attach stream instead of the
  daemon. Same objection as session state: more than one client needs
  the answer and only one holds the stream, so deriving per client
  means several answers to one question.
- **Subscription-gated activity frames.** Complexity with no
  measurable saving; see Wire.
- **Send raw `tool_input` and redact in the daemon.** One place to
  regress, and the secrets are on the socket in the meantime.
- **Hook Pi's `tool_call` / `tool_result`** for richer data. They are
  blocking handlers in the agent's execution path; an observer does not
  belong there.
- **Claude-only v1.** Cheaper, but the schema would be shaped by one
  agent's vocabulary. Pi's lowercase `edit` / `bash` / `read` against
  Claude's `Edit` / `Bash` / `Read` is exactly the kind of difference
  worth discovering before the wire format sets.
