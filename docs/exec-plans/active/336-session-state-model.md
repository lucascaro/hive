# Session state model: know what every agent is doing

- **Spec:** [docs/product-specs/336-session-state-model.md](../../product-specs/336-session-state-model.md)
- **Design:** [docs/design-docs/control-plane.md](../../design-docs/control-plane.md)
- **Issue:** —
- **Branch:** `feature/336-session-state-model`
- **PR:** —
- **Status:** active

## Summary

Give every session a daemon-owned `state` (`working` / `idle` /
`waiting_input` / `waiting_permission` / `exited` / `error`) plus
`last_prompt` / `last_summary`, derived from three sources layered on
one state machine: PTY heuristics for every session, Claude Code hooks
that call `hived hook`, and a Hive-shipped Pi extension. Surface it in
the sidebar, tile headers, tooltips, desktop notifications and
`hivebar`. This plan is written so each phase can be executed by a
separate, smaller agent run; phases are ordered so each one is
shippable and testable on its own.

## Research

Existing plumbing to copy, not reinvent:

- `internal/session/session.go:84` — `Options.Env []string` is appended
  to `os.Environ()` at spawn (`session.go:183`). Nothing sets it today.
  This is where `HIVE_SESSION_ID` / `HIVE_SOCKET` go.
- `internal/session/session.go:225,286,300` — `noteBell` scans each
  delivered chunk and calls the `SetBellHook` callback; the registry
  wires it at `registry.go:278` to `r.noteBell(id)` →
  `broadcastLocked(wire.SessionEventAttention, e.Info())`. The state
  machine hooks in at the same two points (per-chunk + exit).
- `internal/registry/registry.go:73` — `Entry` holds in-memory-only
  fields `Phase`, `NeedsAttention`. New state fields sit beside them and
  are copied into `wire.SessionInfo` in `Entry.Info()` (`registry.go:160`).
- `internal/registry/registry.go:307` — `SetAttention(id, want)` is the
  client-driven "I looked" clear, reached from `FrameUpdateSession`
  (`daemon.go:770`). Keep it; it now also clears `waiting_*` → `idle`.
- `internal/registry/events.go:18,56` — `Subscribe` / `broadcastLocked`
  fan-out. Reuse with a new event kind; do not add a second hub.
- `internal/registry/create.go:109` — the single `spawn(session.Options{…})`
  call; `create.go:420` appends `SessionIDFlag` + id. Agent-specific
  argv/env injection goes next to it.
- `internal/agent/agent.go` — `Def` per agent. Claude has
  `SessionIDFlag: "--session-id"`; Pi has `--session-id` too. Add the
  adapter hooks to `Def` so the registry stays agent-agnostic.
- `internal/wire/control.go:114` — `SessionInfo`; `:299` session event
  kinds (`added`/`removed`/`updated`/`title`/`attention`);
  `internal/wire/frame.go:40-129` — frame ids used through `0x21`.
  `control.go:13` — `Mode` with `control`/`attach`/`create`.
- `internal/daemon/daemon.go:469-473` — HELLO mode switch; `:659`
  `handleControlFrame`. `daemon.go` accepts a connection, reads HELLO,
  dispatches by mode. The `event` mode is a fourth arm.
- `cmd/hived/main.go:27` — flag parsing only; no subcommands. `hived hook`
  is the first. `hived --version --json` shows the pattern for
  "run as a client and exit".
- `internal/buildinfo/contract.go` — `DaemonContract`; bump it (new
  frames + new wire mode = new generation). `scripts/check-daemon-contract.sh`
  enforces the bump.
- `cmd/hivegui/frontend/src/app/state.ts:54-60,162` —
  `needs_attention` on the session type, `attention: Set<string>` in the
  store, `attentionReturnId` for ⌘B/⇧⌘B (spec 240).
  `components/SessionRow.tsx`, `TileChrome.tsx`, `Sidebar.tsx` render the
  pulse. `store/store.ts` holds the reducers.
- `cmd/hivebar/` — reads sessions over one control connection and shows
  an attention count; darwin only.
- `internal/notify/` — desktop notifications, one interface.
- Claude local registry: `~/.claude/sessions/<pid>.json` with keys
  `sessionId`, `status`, `messagingSocketPath`, `pid`, `cwd`, `name`.
  Not needed in this spec (338 uses it) but confirms the id correlation.
- Pi extension API (installed package `docs/extensions.md`):
  `pi.on("session_start" | "input" | "agent_start" | "agent_settled" |
  "session_shutdown")`, `ctx.isIdle()`, `event.text` on `input`,
  `event.messages` on `agent_end`. Loaded with `pi -e <file.ts>`; pi
  compiles TS itself, no build step.

## Approach

One state machine in a new pure package, three feeders, one broadcast.

### Package `internal/agentstate/` (domain layer, no I/O)

```go
type State string   // working | idle | waiting_input | waiting_permission | exited | error
type Source string  // heuristic | hook | extension

type Event struct {
    Kind    string          // see wire.AgentEvent kinds below
    Source  Source
    At      time.Time
    Text    string          // prompt text, summary text, error text (already capped)
}

type Machine struct { /* current State, Source, LastPrompt, LastSummary, lastOutputAt, hookSeenAt */ }

func New(now time.Time) *Machine
func (m *Machine) Output(now time.Time)                 // PTY emitted bytes
func (m *Machine) Bell(now time.Time)
func (m *Machine) Exit(now time.Time, code int)
func (m *Machine) Apply(ev Event) (changed bool)        // hook / extension events
func (m *Machine) Tick(now time.Time) (changed bool)    // quiet-threshold transitions
func (m *Machine) Snapshot() Snapshot                   // State, Source, LastPrompt, LastSummary
```

Rules (table-driven, tested exhaustively):

- **Heuristic tier.** `Output` → `working` (source `heuristic`) unless
  the current source is `hook`/`extension` *and* a hook event arrived
  within `HookStaleAfter` (30 s of the last output without any hook
  event ⇒ demote to heuristic). `Tick` with no output for
  `QuietAfter` (2 s) → `idle`. `Bell` while `idle`/`working` →
  `waiting_input`. `Exit` → `exited` (code ≠ 0 and no prior error →
  still `exited`; `error` is reserved for agent-reported failures).
- **Hook / extension tier.** `Apply` maps event kinds:
  `prompt` → `working` + `LastPrompt` (only if empty, i.e. first prompt),
  `turn_end` → `idle` + `LastSummary`, `waiting_input` → `waiting_input`,
  `waiting_permission` → `waiting_permission`, `permission_resolved` →
  `working`, `error` → `error` + `LastSummary`, `session_end` → `exited`,
  `ping` → no state change.
  Any `Apply` sets `Source` to the event's source and stamps
  `hookSeenAt`. While `Source != heuristic`, `Output` never changes
  `State` (a streaming reply is `working` already; a permission prompt
  repaint must not flip `waiting_permission` back to `working`).
- Text fields capped at `wire.MaxSummaryLen = 512` bytes at the
  boundary (same reasoning as `MaxTitleLen`).
- `Snapshot` is a value; the registry compares before/after to decide
  whether to broadcast.

### Wire (`internal/wire/`)

- `SessionInfo` gains `State string json:"state,omitempty"`,
  `StateSource string json:"state_source,omitempty"`,
  `LastPrompt string json:"last_prompt,omitempty"`,
  `LastSummary string json:"last_summary,omitempty"`. Empty `State` on
  the wire means `idle` (keeps old daemons readable by new clients).
- Constants `State*`, `StateSource*`, `MaxSummaryLen`.
- `SessionEventState = "state"` added to the session event kinds.
- `ModeEvent Mode = "event"`.
- `FrameAgentEvent FrameType = 0x22 // C → S, JSON, event`.
- `type AgentEvent struct { SessionID string; Kind string; Source string; Text string; At string }`
  with kinds `prompt`, `turn_end`, `waiting_input`, `waiting_permission`,
  `ping`, `permission_resolved`, `error`, `session_end`
  (`AgentEventKinds` slice for validation, like `ClientCommands`).
- `PROTOCOL_VERSION` stays 1. `buildinfo.DaemonContract` +1.

### Daemon (`internal/daemon/`)

- HELLO switch: `case wire.ModeEvent:` reads exactly one frame; must be
  `FrameAgentEvent`; validates `Kind ∈ AgentEventKinds`, `Source ∈
  {hook, extension}`, `len(Text) ≤ MaxSummaryLen` (truncate, don't
  reject); calls `registry.ApplyAgentEvent(sessionID, ev)`; closes.
  Unknown session id → log + close (no error frame; the hook has nothing
  useful to do with one). Reject any other frame on an event connection.
- `Config` gains nothing; the socket path is already known. The
  daemon passes `SocketPath` to the registry (`registry.New` option or a
  setter) so `create.go` can put it in the environment.

### Registry (`internal/registry/`)

- `Entry` gains `state *agentstate.Machine` (nil until spawned; `Info()`
  reads `Snapshot()` when non-nil, else zero values).
- `create.go`: after resolving cwd and before `spawn`, build
  `Env: []string{"HIVE_SESSION_ID="+id, "HIVE_SOCKET="+r.socketPath, "HIVE_PROJECT_ID="+projectID}`
  and append `def.SpawnEnv(id)` / `def.SpawnArgs(id, hivedPath)` from the
  agent adapter (below). Wire the session callbacks: extend the existing
  `SetBellHook` pattern with `SetOutputHook(func())` and
  `SetExitHook(func(code int))` on `session.Session` (exit already has a
  path — find where `Alive` flips false and call the machine there).
- `ApplyAgentEvent(id, ev)`: lock, `Apply`, if changed → `broadcastLocked(SessionEventState, info)`.
- A single registry ticker goroutine (`time.Ticker`, 500 ms) calls
  `Tick` on every alive entry; broadcast on change. Stopped in `Close`.
  Output hooks do **not** broadcast on every chunk: they only flip
  `lastOutputAt` and broadcast when the *state* changed (idle → working),
  which is at most once per burst.
- `NeedsAttention` semantics: set when state enters `waiting_*`, or when
  state enters `idle`/`exited`/`error` from `working` (a turn finished).
  `SetAttention(id,false)` (the client's "I looked") additionally moves
  `waiting_input` → `idle` on heuristic sessions only (the hook tier will
  report the real transition itself). Keep `noteBell` — it now calls
  `machine.Bell` and lets the state rules decide.
- Persistence: none. Assert in a test that `MetaFile` JSON is byte-identical
  before/after for a session that went through every state.

### Agent adapters (`internal/agent/`)

`Def` gains two optional funcs, so `create.go` stays agent-agnostic:

```go
// SpawnArgs returns extra argv appended at first spawn AND on
// resume/restart (after SessionIDFlag/ResumeArgs). hivedPath is the
// absolute path of the running daemon binary (os.Executable()).
SpawnArgs func(sessionID, hivedPath, stateDir string) []string
```

- **Claude** (`claude.go`): `SpawnArgs` returns
  `["--settings", <json>]` where the JSON is
  `{"hooks": {<Event>: [{"hooks":[{"type":"command","command":"<hivedPath> hook"}]}]}}`
  for `SessionStart`, `UserPromptSubmit`, `Stop`, `StopFailure`,
  `Notification`, `PermissionRequest`, `PostToolUse`, `SessionEnd`. Build
  the JSON with `encoding/json`, never string concatenation; quote
  `hivedPath` for spaces (macOS app bundle path contains
  `Application Support`). **Open question 1** (hooks merge) must be
  resolved before this lands; the fallback is to read
  `~/.claude/settings.json`'s `hooks` and prepend them.
- **Pi** (`pi.go`, new): `SpawnArgs` returns `["-e", <stateDir>/pi/hive.ts]`.
  The extension source lives at `internal/agent/pi/hive.ts` and is
  embedded with `//go:embed`; `agent.EnsurePiExtension(stateDir)`
  writes it (atomic temp+rename, only if content differs) at daemon
  start — called from `cmd/hived/main.go` right after `agent.SetCustomDir`.
  This is not registry state; it lives under `StateDir()/pi/` the way
  `agents.json` does, and the design rule gets one more named exception.
- Others: nil. Shell: nil.
- **Version gate** (`claude.go`): `claudeVersion()` runs `claude --version`
  once per daemon lifetime (`sync.Once`, 2 s timeout, parse the leading
  semver; failure ⇒ "unknown" ⇒ treat as below minimum). `SpawnArgs`
  returns nil when the version is below `minHooksVersion` or at/above
  `maxKnownBadHooksVersion` (both constants, both commented with the
  reason). Log one line per daemon lifetime when skipped. See the
  design doc's "Surviving Claude Code's churn".

Restart/Revive paths call the same `SpawnArgs` (grep `ResumeArgs` call
sites in `create.go` and the restart file; there are two).

### `hived hook` (`cmd/hived/hook.go`)

`hived hook` (first positional arg before flag parsing; keep `flag`
untouched for the daemon path):

1. Read all of stdin (Claude's hook JSON). Read `HIVE_SESSION_ID`,
   `HIVE_SOCKET` from env; if either is empty exit 0 silently (a user
   running `claude` outside Hive with a copied settings file must not
   see errors).
2. Map `hook_event_name` (+ `notification_type` for `Notification`) to
   an `AgentEvent`:
   - `UserPromptSubmit` → `prompt`, Text = `user_message`
   - `Stop` → `turn_end`, Text = `last_assistant_message`
   - `StopFailure` → `error`, Text = `error_type`
   - `Notification/permission_prompt` → `waiting_permission`
   - `Notification/idle_prompt` → `waiting_input`
   - `PermissionRequest` → `waiting_permission`
   - `PostToolUse` → `permission_resolved` (a tool ran ⇒ the prompt was answered; cheap and exact)
   - `SessionEnd` → `session_end`
   - `SessionStart` → `ping` (no state change; only stamps `hookSeenAt` so the session is promoted to the hook tier before its first prompt)
   - anything else, or a payload missing `hook_event_name` → `ping`
     (unknown/renamed events keep the tier alive without changing state;
     this is the tolerant-parsing rule from the design doc).
3. Dial `HIVE_SOCKET` with a 2 s timeout, `HELLO{mode:event, version}`,
   `AGENT_EVENT`, close. Always exit 0; never print to stdout (Claude
   parses hook stdout for some events). Log failures to stderr only when
   `HIVE_HOOK_DEBUG=1`.
4. Total wall time budget < 100 ms; the hook runs on Claude's turn
   boundary.

### Pi extension (`internal/agent/pi/hive.ts`)

```ts
import net from "node:net";
export default function (pi) {
  const sid = process.env.HIVE_SESSION_ID, sock = process.env.HIVE_SOCKET;
  if (!sid || !sock) return;                     // not under Hive: inert
  const post = (kind, text = "") => { /* open socket, write HELLO + AGENT_EVENT frames, end; swallow errors */ };
  pi.on("session_start", () => post("ping"));
  pi.on("input", (e) => { if (e.source !== "extension") post("prompt", e.text); });
  pi.on("agent_start", () => post("permission_resolved"));
  pi.on("agent_settled", (_e, ctx) => post("turn_end", lastAssistantText(ctx)));
  pi.on("session_shutdown", () => post("session_end"));
}
```

The frame encoding is the same 5-byte header (`type` + `u32 len`) the
Go side uses (`internal/wire/frame.go`) — document it in the file
header so the TS stays in sync; a Go test decodes a fixture written by
the extension's encoder (run `node` in the test only if present; skip
otherwise). `lastAssistantText` walks `ctx.sessionManager` for the last
`assistant` message's text blocks.

Pi has no permission prompt in the Claude sense, so `waiting_permission`
never fires from this tier. `waiting_input` fires when the last
assistant message ends with `?` — a heuristic, flagged in a comment
(`ponytail:` style) as a known ceiling.

### GUI (`cmd/hivegui/frontend/`)

- `app/state.ts`: session type gains `state`, `state_source`,
  `last_prompt`, `last_summary` (read `snake_case ?? camelCase`).
  Selector `sessionState(s)` defaulting to `"idle"`.
- `store/store.ts`: handle `SESSION_EVENT(kind: "state")` like `title`.
  `attention` set stays; it is now fed by `needs_attention` exactly as
  before (daemon changed *when* it flips, not the field).
- `components/SessionRow.tsx`, `TileChrome.tsx`: a `StateDot` component
  (new `components/StateDot.tsx`) taking `state` + `source`; CSS in the
  design system tokens (`docs/design-docs/ui/` — add the six states and
  the heuristic "uncertain" ring treatment). `title` attribute /
  tooltip: `last_prompt` on the first line, `last_summary` on the
  second, `state_source` in the footer.
- `hivebar`: count `state ∈ {waiting_input, waiting_permission}` for
  the "waiting on you" line; fall back to `needs_attention` when
  `state` is empty (old daemon).
- Notifications: the daemon already notifies on attention; change the
  message to name the state ("Claude is waiting for permission in
  *twilight-gate*").

### Files to change

- `internal/wire/control.go` — `SessionInfo` fields, `State*`/`StateSource*`
  consts, `MaxSummaryLen`, `SessionEventState`, `ModeEvent`, `AgentEvent`
  + `AgentEventKinds`.
- `internal/wire/frame.go` — `FrameAgentEvent = 0x22`.
- `internal/buildinfo/contract.go` — `DaemonContract++`.
- `internal/session/session.go` — `SetOutputHook`, `SetExitHook`; call
  sites next to `noteBell` and where the process reaper marks exit.
- `internal/registry/registry.go` — `Entry.state`, `Info()`, ticker,
  `ApplyAgentEvent`, attention rules, `socketPath` field + option.
- `internal/registry/create.go` — env injection, `SpawnArgs`, hooks.
- `internal/registry/<restart file>.go` — same `SpawnArgs` on restart/revive.
- `internal/daemon/daemon.go` — `ModeEvent` arm; pass socket path to registry.
- `internal/agent/agent.go` — `Def.SpawnArgs`; Pi def gets it.
- `internal/agent/claude.go` — hooks settings JSON builder.
- `cmd/hived/main.go` — `hook` subcommand dispatch; `EnsurePiExtension`.
- `cmd/hivegui/frontend/src/app/state.ts`, `store/store.ts`,
  `components/SessionRow.tsx`, `components/TileChrome.tsx`,
  `components/Sidebar.tsx` (tooltip).
- `cmd/hivebar/` — waiting count.
- `DESIGN.md` — Domains: add `internal/agentstate/`; Hard rules: the
  `event` mode is one-frame-and-close; `StateDir()/pi/` exception.
- `docs/design-docs/ui/` — state glyph tokens.

### New files

- `internal/agentstate/machine.go`, `machine_test.go`
- `internal/agent/pi.go` (Pi adapter + embed), `internal/agent/pi/hive.ts`
- `cmd/hived/hook.go`, `hook_test.go`
- `cmd/hivegui/frontend/src/components/StateDot.tsx` + test
- `testdata/claude-hooks/<claude-version>/*.json` — one recorded payload
  per hook event (capture from a real session with `HIVE_HOOK_DEBUG=1`
  once; the version directory is what a drift diff compares against)
- `scripts/probe-claude.sh` — drift probe: scratch `hived` on a temp
  socket + state dir (`HIVE_SOCKET`/`HIVE_STATE_DIR`, never the user's),
  `claude -p "say ok"` launched with Hive's `--settings` JSON, assert
  `prompt` and `turn_end` events arrive within 60 s. Exits 0 with
  "skipped" when `claude` is not on PATH. Wired into `scripts/test.sh`
  behind `HIVE_PROBE_CLAUDE=1` and into the release checklist.

### Tests

- `agentstate`: table test over every (state, input) pair; property
  test that `Snapshot` never reports `working` with source `hook` more
  than `HookStaleAfter` after the last hook event when output continues.
- `wire`: round-trip `AgentEvent`; `SessionInfo` with empty `State`
  decodes to `""` (old daemon compatibility).
- `daemon`: `TestEventModeAcceptsOneFrame`, `TestEventModeRejectsControlFrame`,
  `TestEventModeUnknownSessionDropped`.
- `registry`: `TestOutputBurstBroadcastsOnce`, `TestQuietGoesIdle`,
  `TestBellWhileIdleWaitsInput`, `TestHookOverridesHeuristic`,
  `TestHookStaleFallsBack`, `TestAttentionSetOnWaiting`,
  `TestMetaFileUnchangedByState`, `TestSpawnEnvCarriesHiveIDs`.
- `cmd/hived`: `TestHookMapsEveryEvent` (fixtures → expected
  `AgentEvent`), `TestHookNoEnvExitsZero`, e2e: spawn a fake "agent"
  that is a shell script invoking `hived hook` with a fixture, assert
  `SESSION_EVENT(state)` arrives on a control connection.
- `agent`: `TestClaudeSettingsJSONQuotesPath`, `TestEnsurePiExtensionIdempotent`,
  `TestClaudeVersionGateSkipsBelowMin`, `TestClaudeVersionGateSkipsKnownBad`,
  `TestClaudeVersionUnknownSkips`.
- `cmd/hived`: `TestHookUnknownEventIsPing`, `TestHookMalformedJSONExitsZero`.
- Frontend: vitest for the store reducer and `StateDot`; Playwright
  mock e2e `state-glyphs.spec` driving `SESSION_EVENT(state)` through
  the Wails mock and asserting via `elementFromPoint`-free DOM checks
  (glyph class per state) — CSS validation in a real browser per
  project memory.
- `scripts/test.sh` green; `biome ci .`; `npm run typecheck` after
  `scripts/ci-bootstrap.sh`.

### Phasing (each phase = one PR, one agent run)

1. **Wire + agentstate + heuristic tier.** Everything except adapters,
   `hived hook`, the Pi extension. Sidebar/tile glyphs ship here.
   Shippable: shells and every agent get working/idle/waiting(bell)/exited.
2. **`event` mode + `hived hook` + Claude adapter.** Resolve open
   question 1 first.
3. **Pi extension.**
4. **hivebar + notification wording + tooltip polish.**

## Decision log

- **2026-09-04** — State lives in the daemon, not the GUI. Why: same
  argument as `NeedsAttention` — several clients, one attach stream.
- **2026-09-04** — Hooks post through a wire mode, not a file or second
  socket. Why: `DESIGN.md` single-channel rule; same binary; inherits
  socket auth.
- **2026-09-04** — `error` is reserved for agent-reported failures;
  non-zero exit is `exited`. Why: a shell exiting 1 is not an error state
  the user needs to act on; conflating them makes red dots meaningless.
- **2026-09-04** — Pi extension is embedded in `hived` and written to
  `StateDir()/pi/`, not installed globally. Why: `pi` outside Hive must
  stay untouched; `-e` is per-invocation.
- **2026-09-04** — Text fields capped at 512 bytes at the boundary.
  Why: content is attacker-influenced (any program on the PTY / any
  prompt); same reasoning as `MaxTitleLen`.

## Progress

- **2026-09-04** — Spec and plan written; stage PLAN.

## Open questions

1. **Claude `--settings` hooks merge.** Docs say keys merge; unclear
   whether the `hooks` array concatenates with the user's own hooks or
   replaces them. Test on the installed `claude` before phase 2: put a
   `Stop` hook in `~/.claude/settings.json`, launch with `--settings`
   carrying a different `Stop` hook, observe whether both fire. If they
   replace, the Claude adapter must read and re-emit user hooks
   (settings.json + settings.local.json + project `.claude/settings.json`),
   which is ugly enough to record as a decision.
2. **Bell semantics on hook-tier sessions.** Claude rings the bell on
   `idle_prompt`/`permission_prompt` too. With the hook tier active the
   bell is redundant; the rule above ignores `Bell` while
   `Source != heuristic`. Confirm no double-notification.
3. **Windows.** Unix socket dial from `hived hook` works on Windows only
   if `hived` already uses AF_UNIX there (it does, per `daemon.SocketPath`).
   The Pi extension uses `node:net` which supports the same. Verify in
   the release matrix; no design change expected.
