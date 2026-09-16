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
`PostToolUse` and `PostToolUseFailure`, and collapses all three to
`AgentEventPermissionResolved` — the payload's `tool_name` and
`tool_input` are dropped. No new hook needs wiring and no new process
is spawned; these invocations already happen on every tool call of
every Hive Claude session.

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

**Pi (extension tier) — planned, phase 2.** Nothing below ships in
phase 1; the Pi extension reports no tool or plan events yet. Verified against
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
`todo` tool the Hive extension registers through `pi.registerTool()`,
on by default and disableable in settings. This is Hive adding a tool
to the user's agent, so it is a setting, not a constant.

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
`filepath.Base` in Go, an equivalent in TypeScript that does not assume
`/`. Otherwise the same edit reads `machine.go` on macOS and
`internal\agentstate\machine.go` on Windows.

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

Phase 1 ships only the sidebar row; the inspector panel and activity
grid are planned for phase 3.

- **Inspector panel** beside the terminal in single-session view,
  toggled by key, read-only so the terminal keeps focus.
- **Activity grid**, a keybinding that swaps every grid tile's terminal
  for the same component at tile size.
- **Sidebar row**, which shows only the `SessionInfo` summary.

The layouts are settled and drawn in the round-five mocks. In short:

- **Sidebar** — a small filled `conic-gradient` pie (12px; 11px tight, 10px
  compact) in its own grid cell beneath the state icon, coloured
  by the row's state. It never overlays or restyles the text, so no row
  grows; both existing lines (name and window title) survive untouched.
  At compact density it moves into row 1 beside the icon, widening
  column 1 only for rows that have a plan.
- **Panel** — plan steps with their tools nested beneath. Only the
  current step is expanded by default; the rest collapse to one line
  with a disclosure triangle and a tool-count pill. The full timeline
  scrolls in its own section below.
- **Grid tile** — plan shape as pips, body given to the live tool feed.

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

## Cross-platform

This adds no platform-conditional code. `internal/daemon/socket.go`
uses AF_UNIX on Windows as well — there is no named-pipe split — and
the Claude hooks carrying this data already fire on both platforms
today. The only platform-specific details are the separator-agnostic
label derivation above and the daemon-side duration clock.

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
