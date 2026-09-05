# Session state model: know what every agent is doing

- **Spec:** [docs/product-specs/336-session-state-model.md](../../product-specs/336-session-state-model.md)
- **Design:** [docs/design-docs/control-plane.md](../../design-docs/control-plane.md)
- **Issue:** —
- **Branch:** `feature/336-phase4-hivebar-tooltip` (phases 1–2 shipped
  from `feature/336-session-state-model`, phase 3 from
  `feature/336-phase3-pi-extension`; both merged and dead)
- **PR:** #338 (phases 1–2, merged) · #341 (phase 3, merged) · #344 (phase 4)
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
  machine hooks in at the same per-chunk point. Process exit is
  observed in `Registry.watchSessionExit` (`registry.go:777`), which
  blocks on `sess.Done()` and sets `e.sess = nil` at `:785` —
  `Entry.Alive()` (`:139`) is computed from that, not a field that
  flips. `machine.Exit` is called there, inline.
- `internal/registry/registry.go:73` — `Entry` holds in-memory-only
  fields `Phase`, `NeedsAttention`. New state fields sit beside them and
  are copied into `wire.SessionInfo` in `Entry.Info()` (`registry.go:145`).
- `internal/registry/registry.go:307` — `SetAttention(id, want)` is the
  client-driven "I looked" clear, reached from `FrameUpdateSession`
  (`daemon.go:770`). Keep it; it now also clears `waiting_*` → `idle`.
- `internal/registry/events.go:18,56` — `Subscribe` / `broadcastLocked`
  fan-out. Reuse with a new event kind; do not add a second hub.
- `internal/registry/create.go:109` — the single `spawn(session.Options{…})`
  call; `create.go:420` appends `SessionIDFlag` + id. Agent-specific
  argv/env injection goes next to it. `create.go:511-520` is the
  raw-`Cmd`-from-client branch ("we don't mutate user-supplied cmd"):
  env is still injected there (env is not cmd) but `SpawnArgs` is not.
- `internal/registry/registry.go:546,585` (`Revive`/`ReviveWithPhase`)
  and `:690` (`Restart`) build `opts.Cmd` from `ResumeArgs`/`ResumeCmd`
  (call sites `:637`, `:745`). Both must append `SpawnArgs` too, or a
  restarted Claude session silently drops to the heuristic tier.
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
  `components/SessionRow.tsx` (`:143` already sets `title={sub}` — the
  tooltip pattern to extend), `TileChrome.tsx`, `Sidebar.tsx` render the
  pulse via `hv-state-pulse` (`theme/components/icon.css:36`, driven by
  `--motion-pulse` in `tokens.css:57`) — reuse, don't add a keyframe.
  `store/store.ts` holds the reducers.
  `test/e2e/wails-mock.ts` — the Playwright Wails mock; `MockSession =
  SessionInfo & {…}` and it emits only `added`/`updated`/`removed`
  session events today. It must learn the `state` kind.
- `cmd/hivebar/` — reads sessions over one control connection through
  its own `client.go` / `model.go` types (`model.go:97,195` read
  `NeedsAttention` for the count and marker); darwin only. Those types
  need the new fields threaded through.
- `internal/daemon/daemon.go:483` — HELLO `default:` arm's error text
  says `want control|attach|create`; update the string with the case.
- `internal/notify/` — desktop notifications, one interface. The
  **GUI**, not the daemon, fires them: `cmd/hivegui/app.go:113`
  (`notify.Notify`) on `SessionEventAttention`. The daemon never imports
  `internal/notify`.
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
  Unknown session id, malformed JSON, or any frame type other than
  `FrameAgentEvent` → log + close, no error frame (the hook has nothing
  useful to do with one). Read deadline 2 s on the connection so a
  stalled client cannot pin a goroutine.
- `Config` gains nothing; the socket path is already known. The
  daemon passes `SocketPath` to the registry (`registry.New` option or a
  setter) so `create.go` can put it in the environment.

### Registry (`internal/registry/`)

- `Entry` gains `state *agentstate.Machine` (always non-nil; `Info()`
  reads `Snapshot()`). Revive/restart replace it with `agentstate.New`,
  which is what makes "daemon restart starts every session idle /
  heuristic / empty text" true.
- `create.go`: after resolving cwd and before `spawn`, build
  `Env: []string{"HIVE_SESSION_ID="+id, "HIVE_SOCKET="+r.socketPath}`
  (spec 337 adds `HIVE_PROJECT_ID` when something reads it) and, when
  `spec.Agent` resolves through `agent.Get`, append `def.SpawnArgs(sp)`
  (below). The raw-`Cmd` branch (`create.go:511`) gets env only. Wire
  the per-chunk callback by extending the existing `SetBellHook`
  pattern with `SetOutputHook(func())` on `session.Session`; exit needs
  no new hook — `watchSessionExit` (`registry.go:777`) calls
  `machine.Exit` inline before it nils `e.sess`.
  `Revive`/`ReviveWithPhase`/`Restart` append the same `SpawnArgs` after
  `ResumeArgs`/`ResumeCmd` (`registry.go:637`, `:745`).
- `Entry.state` is created when the entry is registered (boot load or
  create), never nil, so an `AGENT_EVENT` that races the spawn (entry
  exists, `Phase != Ready`) is applied, not dropped and not a nil deref.
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
// SpawnInfo is what an adapter may need at spawn time. Only the fields
// an adapter reads are guaranteed non-empty; an empty field means
// "unavailable, skip your surface".
type SpawnInfo struct {
    HivedPath string // absolute path of the running daemon (os.Executable()); "" if unresolvable
    StateDir  string // registry.StateDir()
}
// SpawnArgs returns extra argv appended at first spawn AND on
// resume/restart (after SessionIDFlag/ResumeArgs/ResumeCmd).
SpawnArgs func(sp SpawnInfo) []string
```

`HivedPath` is resolved once at daemon start (`os.Executable()`,
`filepath.EvalSymlinks`); on error it is `""` and the Claude adapter
returns nil (log once). The session id is not passed: Claude gets it via
`SessionIDFlag`, Pi via its own `--session-id`.

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
- **Pi** (`pi.go`, new): `SpawnArgs` returns `["-e", <stateDir>/pi/hive.ts]`
  only if that file exists (`os.Stat`); otherwise nil (log once).
  The extension source lives at `internal/agent/pi/hive.ts` and is
  embedded with `//go:embed`; `agent.EnsurePiExtension(stateDir)`
  writes it (atomic temp+rename, only if content differs) at daemon
  start — called from `cmd/hived/main.go` right after `agent.SetCustomDir`.
  A write failure is logged and **does not** stop the daemon; the Pi
  adapter's `os.Stat` check above is what turns that into "heuristic
  tier for Pi" instead of a broken spawn.
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

Restart/Revive paths call the same `SpawnArgs` (`registry.go:637` and
`:745`; both are in `registry.go`, there is no separate restart file).

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
3. Dial `HIVE_SOCKET` with a 2 s timeout, set a 2 s write deadline on
   the conn, write `HELLO{mode:event, version}` + `AGENT_EVENT`, close
   without waiting for a reply (the daemon sends none). Always exit 0;
   never print to stdout (Claude parses hook stdout for some events).
   Malformed/empty stdin → `ping`. Log failures to stderr only when
   `HIVE_HOOK_DEBUG=1`.
4. Wall time is < 100 ms on the happy path; the deadlines above are the
   hard ceiling when the daemon is gone or wedged.

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
  the heuristic "uncertain" ring treatment); reuse `hv-state-pulse` /
  `--motion-pulse` for the waiting states.
  **SUPERSEDED (phases 1 and 4):** no `StateDot.tsx` was built (the
  existing `StateIcon` already had the job), and the "uncertain" ring
  was never built either — the tiers differ only in the tooltip's last
  line. Both decisions are in the Decision log; this paragraph is kept
  as the original intent, not as a description of the tree. `title` attribute /
  tooltip: `last_prompt` on the first line, `last_summary` on the
  second, `state_source` in the footer.
- `hivebar` (`cmd/hivebar/client.go`, `model.go`): thread `state` /
  `state_source` through its own session types; count
  `state ∈ {waiting_input, waiting_permission}` for the "waiting on
  you" line; fall back to `needs_attention` when `state` is empty (old
  daemon).
- Notifications: `cmd/hivegui/app.go:113` fires `notify.Notify` on
  `SessionEventAttention`; change the message there to name the state
  ("Claude is waiting for permission in *twilight-gate*"). The daemon
  stays out of notifications.

### Files to change

- `internal/wire/control.go` — `SessionInfo` fields, `State*`/`StateSource*`
  consts, `MaxSummaryLen`, `SessionEventState`, `ModeEvent`, `AgentEvent`
  + `AgentEventKinds`.
- `internal/wire/frame.go` — `FrameAgentEvent = 0x22`.
- `internal/buildinfo/contract.go` — `DaemonContract++`.
- `internal/session/session.go` — `SetOutputHook`; call site next to
  `noteBell` (`:225`).
- `internal/registry/registry.go` — `Entry.state`, `Info()` (`:145`),
  ticker, `ApplyAgentEvent`, attention rules, `socketPath` field +
  option, `machine.Exit` in `watchSessionExit` (`:777`), `SpawnArgs` in
  `Revive`/`ReviveWithPhase`/`Restart` (`:637`, `:745`).
- `internal/registry/create.go` — env injection, `SpawnArgs` (agent
  branch only), hooks.
- `internal/daemon/daemon.go` — `ModeEvent` arm (`:469-473`), update the
  `default:` error text (`:483`); pass socket path to registry.
- `internal/agent/agent.go` — `SpawnInfo`, `Def.SpawnArgs`; Pi def gets it.
- `internal/agent/claude.go` — hooks settings JSON builder, version gate.
- `cmd/hived/main.go` — `hook` subcommand dispatch; `EnsurePiExtension`;
  `HivedPath` resolution.
- `cmd/hivegui/app.go` — notification text names the state (`:113`).
- `cmd/hivegui/frontend/src/app/state.ts`, `store/store.ts`,
  `components/SessionRow.tsx`, `components/TileChrome.tsx`,
  `components/Sidebar.tsx` (tooltip), `test/e2e/wails-mock.ts` (emit
  `state` events).
- `cmd/hivebar/client.go`, `cmd/hivebar/model.go` — new fields, waiting count.
- `DESIGN.md` — Domains: add `internal/agentstate/`; Hard rules: the
  `event` mode is one-frame-and-close; `StateDir()/pi/` exception.
- `docs/design-docs/ui/` — state glyph tokens.
- `README.md` — "What works" gains the state line.
- `.changesets/336-session-state-model.md` — required by CI
  (AGENTS.md changeset rule); one entry per phase PR.

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
  `prompt` and `turn_end` events arrive within 60 s, and that a
  project-level `Stop` hook in the scratch dir still fires alongside
  Hive's (hooks concatenate). Exits 0 with
  "skipped" when `claude` is not on PATH. Wired into `scripts/test.sh`
  behind `HIVE_PROBE_CLAUDE=1` and into the release checklist.

### Tests

- `agentstate`: table test over every (state, input) pair; property
  test that `Snapshot` never reports `working` with source `hook` more
  than `HookStaleAfter` after the last hook event when output continues.
- `wire`: round-trip `AgentEvent`; `SessionInfo` with empty `State`
  decodes to `""` (old daemon compatibility).
- `daemon`: `TestEventModeAcceptsOneFrame`, `TestEventModeRejectsControlFrame`,
  `TestEventModeUnknownSessionDropped`, `TestEventModeMalformedJSONDropped`,
  `TestEventModeReadDeadline`.
- `registry`: `TestOutputBurstBroadcastsOnce`, `TestQuietGoesIdle`,
  `TestBellWhileIdleWaitsInput`, `TestHookOverridesHeuristic`,
  `TestHookStaleFallsBack`, `TestAttentionSetOnWaiting`,
  `TestAttentionClearMovesWaitingToIdleHeuristicOnly`,
  `TestMetaFileUnchangedByState`, `TestSpawnEnvCarriesHiveIDs`,
  `TestRawCmdGetsEnvNotSpawnArgs`, `TestRestartAppendsSpawnArgs`,
  `TestReviveStartsIdleHeuristicEmptyText`, `TestAgentEventBeforeReadyApplied`.
- `cmd/hived`: `TestHookMapsEveryEvent` (fixtures → expected
  `AgentEvent`), `TestHookNoEnvExitsZero`, e2e: spawn a fake "agent"
  that is a shell script invoking `hived hook` with a fixture, assert
  `SESSION_EVENT(state)` arrives on a control connection.
- `agent`: `TestClaudeSettingsJSONQuotesPath`, `TestEnsurePiExtensionIdempotent`,
  `TestClaudeVersionGateSkipsBelowMin`, `TestClaudeVersionGateSkipsKnownBad`,
  `TestClaudeVersionUnknownSkips`, `TestClaudeSpawnArgsNilWithoutHivedPath`,
  `TestEnsurePiExtensionRewritesOnDiff`, `TestPiSpawnArgsNilWhenFileMissing`.
- `cmd/hived`: `TestHookUnknownEventIsPing`, `TestHookMalformedJSONExitsZero`,
  `TestHookWriteDeadlineExitsZero` (daemon accepts, never reads),
  `TestEnsurePiExtensionFailureDoesNotStopDaemon`.
- Manual checklist (phase 2 deliverable, appended to this plan's
  Progress with the Claude version): each of the six states observed on
  a real `claude` session launched from Hive.
- Frontend: vitest for the store reducer and `StateDot`; Playwright
  mock e2e `state-glyphs.spec` driving `SESSION_EVENT(state)` through
  the Wails mock and asserting via `elementFromPoint`-free DOM checks
  (glyph class per state) — CSS validation in a real browser per
  project memory.
- `scripts/test.sh` green; `biome ci .`; `npm run typecheck` after
  `scripts/ci-bootstrap.sh`.

## Verification

Run from the repo root of the worktree. Fresh worktree first:

```sh
scripts/ci-bootstrap.sh                 # pinned Wails CLI + generated bindings
```

Then, per phase:

```sh
go build ./... && go vet ./...
go test ./internal/... ./cmd/hived/...
scripts/check-daemon-contract.sh        # the bump is required by phase 1
scripts/test.sh                         # go · unit · dom · e2e (mock)
( cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && CI=1 npx playwright test )
```

Phase 2 additionally: the live probe (one API call; skips cleanly when
`claude` is not on PATH) and the manual checklist above.

```sh
HIVE_PROBE_CLAUDE=1 HIVE_DEBUG_STATE=1 go test ./cmd/hived/ -run TestClaudeProbe -v
``` Phase 3: `node --test internal/agent/pi/` when `node` is present.

### Phasing (each phase = one PR, one agent run)

1. **Wire + agentstate + heuristic tier.** Everything except adapters,
   `hived hook`, the Pi extension. Sidebar/tile glyphs ship here.
   Shippable: shells and every agent get working/idle/waiting(bell)/exited.
2. **`event` mode + `hived hook` + Claude adapter.** Resolve open
   question 1 first.
3. **Pi extension.**
4. **hivebar + notification wording + tooltip polish.**

### Manual smoke checklist

Run against an iso build (`wails build`, never `-s`) with the daemon
started as `HIVE_DEBUG_STATE=1`. Every row is pass/fail with the log
line that proves it; "looks fine" is not a result. Record the Claude
version and the table in Progress before each push that touches state.

| # | Session | Action | Expected glyph (sidebar = tile) | Expected log line |
|---|---------|--------|----------------------------------|-------------------|
| 1 | shell | `sleep 1; ls -R /usr/lib \| head -200` | working during output, idle ≤ 3 s after | `idle -> working reason=output`, `working -> idle reason=tick` |
| 2 | shell | `printf '\a'` while the session is NOT active | waiting_input pulse; OS notification; sidebar row lit | `-> waiting_input reason=bell` |
| 3 | shell | switch to that session | idle; row unlit | `waiting_input -> idle reason=clear` |
| 4 | shell | `printf '\a'` while active + window focused, do nothing 10 s | stays waiting_input (no self-clear); no OS notification | one `bell` line, NO `clear` line |
| 5 | shell | then press any key | idle | `reason=clear` |
| 6 | shell | `exit` | exited (hollow grey) | `-> exited reason=exit` |
| 7 | claude | launch, sit at prompt 30 s | idle after startup paint; `state_source=hook` after SessionStart | `-> idle reason=tick`, then `reason=ping src=hook` |
| 8 | claude | type a prompt, Enter | working within 1 s | `-> working src=hook reason=prompt` |
| 9 | claude | ask it to run `ls` with Bash (default permissions) | waiting_permission (distinct colour) + notification | `-> waiting_permission src=hook reason=waiting_permission` |
| 10 | claude | allow | working, then idle when the reply ends | `reason=permission_resolved`, `-> idle reason=turn_end` |
| 11 | claude | leave it idle > 60 s after a reply | waiting_input (Claude `idle_prompt`) | `reason=waiting_input src=hook` |
| 12 | claude | `/exit` | exited | `-> exited reason=session_end` or `reason=exit` |
| 13 | claude | kill `hived`, restart, reopen GUI | every session idle / heuristic, empty tooltip | no state lines until output |
| 14 | codex or pi | run a prompt | codex: working → idle, `src=heuristic`. pi: working → idle with `src=extension` and the prompt in the tooltip — the glyph itself is identical on every tier, so read the tooltip's last line, not the shape | `src=heuristic` (codex) / `src=extension` (pi) |
| 15 | claude + shell | with one agent at a permission prompt and one shell idle, open the menu bar | summary reads "… · 1 waiting on you"; the dot is on the agent row only | — (hivebar has no state log; read the menu) |
| 16 | claude | hover the sidebar row's state glyph mid-turn, then after the reply | tooltip stacks state, the prompt in quotes, the last summary, then "reported by the agent"; a shell's tooltip ends "guessed from terminal output" | — |

Rows 7–12 are phase 2; rows 15–16 are phase 4. A row that fails goes into this plan as a
decision-log entry with the log excerpt BEFORE any code changes.

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

- **2026-09-04** — `HIVE_PROJECT_ID` deferred to spec 337. Why: nothing
  in 336 reads it; inject env when a reader exists.
- **2026-09-04** — `SpawnArgs` takes a `SpawnInfo` struct, not the
  session id. Why: neither adapter needs the id (both get it from their
  own flag); a struct lets a field be "unavailable" without a signature
  change.
- **2026-09-04** — No `SetExitHook`. Why: one caller; the registry
  already owns the exit path in `watchSessionExit`.
- **2026-09-04** — Bell is ignored while `Source != heuristic`. Why:
  Claude rings the bell on the same prompts its hooks report; honouring
  both double-notifies. Resolves former open question 2.
- **2026-09-04** — Windows needs no design change: `daemon.SocketPath`
  already yields an AF_UNIX path under `%LOCALAPPDATA%\Hive`, which
  both `hived hook` and `node:net` can dial. Resolves former open
  question 3; verified in the release matrix.
- **2026-09-04** — `--settings` `hooks` **concatenate** with hooks from
  other settings sources; they do not replace them. Verified on Claude
  Code 2.1.260: a project `.claude/settings.json` `Stop` hook and a
  `--settings '{…}'` `Stop` hook both fired on one `-p` turn. So the
  Claude adapter emits only Hive's hooks and never has to read the
  user's settings. `scripts/probe-claude.sh` repeats this exact check
  (two `Stop` hooks, both must fire) so a future change in merge
  semantics shows up as a probe failure, not a silent loss of the
  user's hooks. Resolves former open question 1.
- **2026-09-04 (phase 1)** — Every machine mutator returns `changed
  bool`; the registry broadcasts on that rather than diffing snapshots
  before/after. Why: the plan specified both mechanisms (a bool from
  `Apply`/`Tick`, a snapshot diff elsewhere) and two ways to answer one
  question is how they drift. `Snapshot` remains, for `Entry.Info()`.
- **2026-09-04 (phase 1)** — `Entry.state` is created lazily by
  `machine()` rather than assigned at every construction site. Why:
  there are four such sites (two disk-load paths, create, and the test
  helpers) and a missed one is a nil deref, not a missing dot.
  `Entry.Info()` reads through a separate read-only `stateSnapshot()`
  whose zero value *is* idle-on-the-heuristic-tier, so the two call
  sites that render an entry after releasing `r.mu` stay race-free.
- **2026-09-04 (phase 1)** — The heuristic tier never raises
  `NeedsAttention` on its own; only an agent-reported transition does,
  and the bell keeps doing exactly what it did before. Why: the plan's
  rule ("attention when state enters idle from working") is true of
  every `ls` in every shell, and that flag drives desktop
  notifications. Phase 1 ships only the heuristic tier, so following
  the plan literally would have made the flag worthless on day one.
  `wantsAttention` in `registry.go` holds the line.
- **2026-09-04 (phase 1)** — `SetAttention` gained `ClearWaiting` on the
  machine rather than reusing `Apply(turn_end)`. Why: "the user looked"
  is not something an agent reported, so it must not touch the tier or
  the staleness clock — and `turn_end` would also clear `LastSummary`.
- **2026-09-04 (phase 1)** — No `StateDot.tsx`. The GUI already resolves
  a session to one of five icon shapes (`lib/session-state.ts` +
  `components/Icon.tsx` `StateIcon`), used by the sidebar row, the
  minimized chip and the tile header. Phase 1 extends that union with
  `working` and `waiting-permission` instead of adding a second,
  parallel state component. Both new shapes reuse the existing
  `--state-running` / `--state-attention` tokens: those are already
  picked for all 18 themes, and shape plus motion carries the
  difference without asking anyone to pick 18 more colours.
- **2026-09-04 (phase 1)** — `STATE_WORDS.running` now reads "Idle",
  not "Running". Why: with a real `working` state beside it, "Running"
  described the wrong one of the two.
- **2026-09-04 (phase 1)** — Session-state fields on the JS side are
  single-spelled `snake_case`, matching `title` and `needs_attention`
  in the same interface, not the `snake_case ?? camelCase` pair the
  plan called for. Why: `SessionInfo` reaches the frontend as raw wire
  JSON, where the daemon's struct tags are the only spelling that
  exists; the camelCase fallbacks in that file are for Wails-bound
  types, which this is not.
- **2026-09-04 (phase 1)** — `hivebar` and the notification wording are
  untouched. Why: the phasing puts both in phase 4, and threading
  fields no reader uses is scope with no payoff. `hivebar` keeps
  working off `needs_attention`, whose semantics phase 1 did not change.
- **2026-09-04 (phase 1)** — Env injection (`HIVE_SESSION_ID` /
  `HIVE_SOCKET`) is deferred to phase 2 with the `event` wire mode.
  Why: pointing an agent at a socket that has no `event` arm to dial is
  a lie the daemon would have to keep for a release.
- **2026-09-04 (phase 1, after smoke test, SUPERSEDED below)** — The
  heuristic tier applies to shell sessions only (`Entry.Agent == ""`).
  Why: measured on a live session, an idle Claude Code emits an
  `ESC[?6n` cursor-position query every 200 ms — 61 DATA frames in
  12 s, max gap 0.21 s, zero visible characters. The plan's premise
  ("no bytes for QuietAfter ⇒ idle") is therefore false for an agent
  TUI: the session reads as permanently `working`, and a bell's
  `waiting_input` is overwritten by the next poll before any client can
  paint it. A bare `claude` in a plain PTY is silent when idle — it
  only polls when something answers — which is why no test caught it.
  Alternatives considered and rejected: a screen-content fingerprint
  sampled on the tick (correct for any agent, but a bigger change than
  phase 1 warrants), and a denylist of no-op sequences (silently breaks
  on the next agent that polls differently). Phases 2–3 make the
  question moot for the agents that matter.
- **2026-09-04 (phase 1, second smoke test)** — Supersedes the descope
  above: the heuristic tier applies to every session again, but it is
  driven by a **screen digest** (`session.VT.ScreenDigest`, an FNV hash
  of every visible cell's rune and attributes) sampled on the existing
  500 ms tick, not by byte arrival. Working means the rendered screen
  changed; idle means it stopped changing for `QuietAfter`.
  Why: descoping was correct about the measurement and wrong about the
  remedy — it left the flagship agent with no state at all, which the
  user rejected on sight ("claude is doing something and it shows
  idle"). The digest answers the question the plan meant to ask. The
  `ESC[?6n` flood changes no cell, so it reads as idle; a streaming
  reply changes cells, so it reads as working. Cost is one hash per
  live session per tick, and the per-chunk output hook
  (`session.SetOutputHook`, `registry.noteOutput`) is deleted with it —
  that was taking `r.mu` at PTY speed.
- **2026-09-04 (phase 1, second smoke test)** — `Machine.Output` no
  longer leaves `waiting_input` / `waiting_permission`; only
  `ClearWaiting` (the client reporting that the user looked) does.
  Why: an agent that rings and then keeps redrawing was burying its own
  request for attention within one tick. This matches what
  `NeedsAttention` has always done, so the two can no longer disagree.
- **2026-09-04 (phase 1, third smoke test)** — `needs_attention` is
  **derived**, not stored: `needsAttention(state) = state ∈
  {waiting_input, waiting_permission}`. `Entry.NeedsAttention` is gone,
  and the GUI no longer writes its local `attention` set at all — the
  daemon's `attention` event and the session list are its only writers.
  Why: this is the root cause of every regression in this feature.
  "Wants the user" and "what the session is doing" were two independent
  pieces of daemon state saying overlapping things, and each client kept
  a third copy it wrote to optimistically. Three answers to one
  question, and they diverged in both directions — a session lit up
  forever because the GUI cleared its copy and told nobody
  (`focus.ts` called `clearAttentionFor`, the local-only one, on every
  session switch), and a bell looked delayed because the local copy was
  suppressed while the daemon's was not. One owner ends the class.
  The GUI still decides one thing locally, and correctly: whether to
  post an OS notification. That is a judgement about the person, not
  about the session.
- **2026-09-04 (phase 1, third smoke test)** — A wait is ended by user
  *input* to the session (`noteUserInput`, on the xterm `onData` edge),
  not by window focus. Why: a focused window can sit untouched for an
  hour, and a bell arriving while the session is already active fires no
  focus event at all — so there was no way out of the flag. Typing is
  the one unambiguous "I have seen this". An earlier attempt cleared on
  the bell itself when the session was active and focused, which made
  `printf '\a'` in the session you are watching raise nothing at all;
  that is a request being discarded rather than answered.
- **2026-09-04 (phase 1, fourth smoke test)** — `setActive` reports "the
  user looked" only on an actual switch (`id !== activeId`), not on
  every call. Why: it is a choke point many paths re-enter for the
  session that is *already* active — a grid move, a project switch, a
  re-render — and once the clear became a daemon RPC, each of those
  wiped a bell before it could be seen. Symptom: `printf '\a'` in the
  shell you were watching cleared itself instantly and the state fell
  through waiting_input → working (the shell's next prompt) → idle.
  Arriving at a session is the signal; being parked in one is not, and
  `noteUserInput` covers that case.
- **2026-09-04 (phase 1, third smoke test)** — FOLLOW-UP for phase 2:
  `turn_end` currently maps to `idle`, so an agent finishing a turn no
  longer raises attention (it used to, via the separate flag). Under the
  derived model the honest mapping is `turn_end → waiting_input` — an
  agent that has finished IS waiting for you. Decide it with the hook
  tier, where a real `turn_end` exists to test against.
- **2026-09-04 (phase 1, second smoke test)** — Test coverage was the
  real defect. Every bell test called `registry.noteBell` directly,
  which skips the bell scanner, the hook installation, and the tier
  decision — the entire chain a user depends on. `state_test.go` now
  drives real PTYs end to end: `TestBellReachesAttentionThroughTheRealPTY`
  (both tiers), `TestTerminalQueriesAreNotWork` (a child emitting
  `ESC[?6n` at 20 Hz must still go idle), `TestVisibleOutputIsWork`,
  `TestBellWaitsUntilTheUserLooks`. Two harness traps found while
  writing them, both worth remembering: a bare `\a` written into a PTY
  never reaches the scanner (`cat` is line buffered), and bytes written
  *into* a PTY are echoed by the line discipline as visible text, so a
  query must be emitted by the child to test as a query.
- **2026-09-04 (phase 1, after smoke test)** — The tile header renders
  from the session list, not from `TileChromeState.info`. Why: that
  snapshot was refreshed only when the layout ran `ensureTerm()`, so a
  `SESSION_EVENT(state)` repainted the sidebar and left the tile stale —
  the same session disagreeing with itself. `info` had exactly one
  consumer, so the field is deleted rather than kept in sync.
  `chrome.phase` stays: it is genuinely the tile's own.
- **2026-09-04 (phase 1, after smoke test)** — Same for the window
  title: `TileChromeState.termTitle` and `SessionTerm.termTitle` are
  deleted in favour of `SessionInfo.title`. Why: the local copy came
  from that tile's own xterm `onTitleChange`, so a tile that was never
  attached or was rebuilt had none — the reported "tile titles
  sometimes don't work". Pre-existing, not introduced by 336, and
  carries its own changeset.
- **2026-09-04** — Desktop notifications stay in the GUI
  (`cmd/hivegui/app.go`), the daemon never imports `internal/notify`.
  Why: that is where they live today; moving them is out of scope.

- **2026-09-04** — **Transition table frozen.** Three rewrites of the
  clear rule (focus → bell → keystroke) each moved the bug rather than
  fixing it. The table below is the contract; a change to it is a
  decision-log entry with sign-off, not a fix commit.

  Heuristic tier (every session; `state_source` empty):

  | From | Observation | To | Reason tag |
  |------|-------------|----|------------|
  | idle | screen digest changed | working | `output` |
  | working | digest unchanged for `QuietAfter` (2 s) | idle | `tick` |
  | idle, working | BEL | waiting_input | `bell` |
  | waiting_input | digest changed | waiting_input (no change) | — |
  | waiting_input | user typed into it, or switched *to* it | idle | `clear` |
  | any but exited | child exited (any code) | exited | `exit` |
  | any | anything | error | never — `error` is agent-reported only |

  Agent tiers (`hook` / `extension`): `Apply` in `agentstate/machine.go`
  is the table. While the tier is fresh (`HookStaleAfter` = 30 s) output
  is ignored; past it, output demotes to heuristic `working`. BEL is
  honoured on every tier (idle/working → waiting_input, tier unchanged)
  and a keystroke clears waiting_input AND waiting_permission on every
  tier (amended again after a declined question tool left a session
  lit "waiting for permission" with no agent event to end it; clearing
  waiting_permission lands on `working`, because Claude fires no hook
  between the allow and the tool finishing — PreToolUse runs before the
  dialog, measured on 2.1.261): a hooked Claude
  rings when its turn finishes and Stop maps to idle, so the bell is the
  only "come look" a finished turn produces — the alert users have
  today. Amended 2026-09-04 after the user reported exactly that
  regression; the first cut ignored the bell on trusted tiers.

  Derived, never stored: `needs_attention = state ∈ {waiting_input,
  waiting_permission}`. Owner is the daemon; clients keep no copy. The
  GUI decides one thing locally — whether to post an OS notification —
  and that is on the attention edge, only when the session is not active
  or the window is unfocused.

  Evidence rule: `HIVE_DEBUG_STATE=1 hived` logs
  `state: <id> <prev> -> <next> src=<tier> reason=<tag>`. A "still
  wrong" report without that transcript is not actionable.

- **2026-09-05 (phase 3)** — The Pi extension reports
  `waiting_permission` / `waiting_input` from `ui_prompt_start` (and
  `permission_resolved` from `ui_prompt_end`), not from the planned
  "last assistant message ends with `?`" heuristic, which is deleted.
  Why: pi 0.85.0's `docs/extensions.md` documents `ui_prompt_start` /
  `ui_prompt_end` as notification-only events fired around every
  blocking extension UI prompt (`ctx.ui.select/confirm/input/editor/
  custom`), coalescing nested prompts into one outer waiting span —
  precisely "the session is waiting for the user", reported by pi
  itself. The plan assumed Pi had no such surface because it has no
  built-in permission prompt; that is true (a permission gate in Pi is
  an extension calling `ctx.ui.confirm()`) but irrelevant, because the
  prompt those gates raise fires these events. `confirm`/`select` map
  to `waiting_permission` and every other kind to `waiting_input`. This
  removes the one deliberate ceiling phase 3 was going to carry, so no
  `ponytail:` comment is warranted.

- **2026-09-05 (phase 3)** — The extension's frame encoder is checked
  from Go by running real `node`, not by a hand-written byte fixture.
  Why: `hive.ts` hand-rolls the 5-byte header because it is the only
  encoder of Hive frames outside the `internal/wire` package, and a
  fixture is one more copy of the thing that can drift. `encodeFrames`
  is exported for exactly this, and the Go test decodes its live output
  with `wire.ReadFrame` + the daemon's own `AgentEventKinds` allowlist,
  so a header change on either side fails the build. Both
  node-dependent tests skip when no node that can *load TypeScript* is
  available — see the correction below; "CI has node" was the wrong
  test.

- **2026-09-05 (phase 3)** — `hive.ts` is written in erasable-syntax
  TypeScript (type annotations and `import type` only, no enums, no
  parameter properties) so node's default type stripping loads it
  directly. Why: pi loads extensions through jiti and needs no build
  step, but the tests do — `node --test pi/hive.test.ts` and the Go
  frame test both import the extension as-is. Keeping the syntax
  erasable is what lets the tested artifact be the shipped artifact
  rather than a compiled copy of it.


- **2026-09-05 (phase 3, after review iter 1)** — CI's node pin moves 20
  → 24 (`.github/workflows/ci.yml:60`, `pages.yml:39`) and the tests'
  node guard becomes capability-based instead of presence-based. Why:
  the guard shipped as `exec.LookPath("node")`, which answers "is there
  a node" — the wrong question. Node strips types only from 22.6 (behind
  `--experimental-strip-types`) and 23.6 (by default), CI pinned 20, and
  both node-backed tests failed `ERR_UNKNOWN_FILE_EXTENSION` on all
  three legs while passing locally on node 24. The verification claim in
  the phase-3 progress entry was therefore true locally and false in CI,
  which is exactly the class of miss the plan's "record the log line
  that proves it" rule exists to stop.
  The guard now writes a one-line `.ts` file and runs it, so it probes
  the behaviour rather than parsing a version string — node has moved
  this default twice already. A contributor on an older node gets a
  skip; CI, pinned to 24, actually runs them. Node 20 reached end of
  life in April 2026, so the bump is overdue on its own merits; the
  frontend toolchain (Vite, Vitest, Playwright, tsc) is current enough
  to be unaffected.
  Considered and rejected: shipping the extension as plain `.js`, which
  would make every node version work and need no CI change (verified: pi
  0.85.0 loads a `.js` extension and exits 0). Rejected because pi's
  documented idiom is TypeScript and `import type { ExtensionAPI }` is
  the only thing tying the file to the API it implements. Rejected too:
  a capability guard *without* the CI bump, which goes green by never
  running the cross-language frame check in CI — the one place it earns
  its keep.

- **2026-09-05 (phase 3, review iter 2)** — `session_shutdown` reports
  `session_end` only for `reason == "quit"`; every other reason
  (`new`, `resume`, `fork`, `reload`) reports `turn_end` with no text.
  Why: only `quit` ends the pi process. The other four tear the session
  runtime down *inside a live pi* and immediately stand another one up,
  and `StateExited` is terminal in `Machine.Apply` (it returns early on
  every later event), so an ordinary `/new` would have pinned the Hive
  session at "exited" for the rest of its life with the PTY still
  running. `turn_end` rather than nothing, because the command arrives
  as an `input` event first and has already moved the session to
  `working`; posting nothing would strand it there until the tier went
  stale. Empty text clears `lastSummary`, which is correct — the
  previous conversation's closing line does not describe the new one.
  Known ceiling, marked `ponytail:` in the extension: `lastPrompt`
  survives the swap, because `Apply` sets it once and no wire kind
  resets it. Clearing it needs a session-reset kind, i.e. a wire change.

- **2026-09-05 (phase 3, review iter 2)** — Event ordering is enforced
  in `agentstate.Machine.Apply`, not in the Pi extension: an event
  stamped before the last one applied is dropped whole, staleness clock
  included. Why: every reporting tier is one short-lived connection per
  event served on its own goroutine, so an inverted pair is possible on
  the Claude hook tier (shipped in #338) exactly as much as on the new
  Pi one — more so, since each Claude event is a separate process.
  Serializing `post()` inside the extension was the narrower fix and was
  rejected: it would have left the hook tier racy, and a chained post
  can still be in flight when pi exits, trading a rare ordering bug for
  a rare dropped-event bug. The reporter always shares the daemon's host
  (it dialled a unix socket), so comparing its timestamps is sound.
  Equal stamps still apply, since nothing distinguishes them.


- **2026-09-05 (phase 3, review iter 3)** — `Registry.ApplyAgentEvent`
  clamps a forward-dated reporter stamp to `time.Now()`. Why: iter 2's
  ordering guard made an old weakness permanent. `ev.At` arrives from
  the reporter and was parsed unbounded; `Machine.Apply` now orders by
  it and `trusted()` already measured staleness from it, so a single
  future-dated event froze the session for its whole life — every later
  event sorted older and was dropped, while `now.Sub(hookSeenAt)` stayed
  negative so the heuristic tier could not reclaim it either. One bad
  stamp must cost one event, not the session. Clamping (rather than
  refusing the event) keeps the wire doc's stated promise that a clock
  the daemon does not control cannot drop an otherwise-valid event.
  Backward-dated stamps are still honoured as-is: that is the ordering
  signal the guard exists to read.

- **2026-09-05 (phase 3, review iter 3)** — The Pi extension stamps `at`
  when it observes the event, not inside the `connect` callback. Why:
  stamping at connect time makes the stamp track connect order, which is
  the same order delivery already has — so the daemon's ordering guard
  would have been close to inert for the tier it was added for, and an
  inverted connect pair would have made it permanently drop the
  semantically newer event. It also broke `wire.AgentEvent`'s documented
  contract ("At is when the reporter observed this").


- **2026-09-05 (phase 3, review iter 4)** — The ordering guard's window
  is bounded by `HookStaleAfter`: an event is dropped only when it is
  behind the watermark by *less* than that. Why: `ev.At` is parsed off
  the wire and carries no monotonic reading, so this is wall-clock
  arithmetic. With an unbounded guard, a host clock that stepped
  backward (VM resume, a large NTP correction) would drop every
  subsequent event for the length of the step, while `trusted()`
  computed a negative age that never exceeds `HookStaleAfter` — so
  `Observe` and `Tick` could not reclaim the session either, and nothing
  would correct it. That was a regression introduced by iter 2's guard:
  before it, the stuck-tier half existed but the next report still
  applied and unstuck the state. A delivery inversion is milliseconds
  apart; a larger backward gap is a clock that moved, and the newest
  report wins. Both sides are pinned
  (`TestApplyRecoversFromBackwardClockStep`,
  `TestApplyStillDropsARealInversion`).


- **2026-09-05 (phase 4)** — hivebar decides "is this session waiting"
  from the daemon's contract number, NOT from whether `state` is empty.
  The Approach above says to "fall back to `needs_attention` when
  `state` is empty (old daemon)", and that rule is wrong: `StateIdle`
  IS the empty string, so an idle session on a current daemon and every
  session on a pre-contract-3 daemon are byte-identical on the wire.
  Reading it literally would have made an old daemon report a permanent
  zero and — worse — made every idle session on a current one fall
  through to the bell flag, resurrecting exactly the "rang once an hour
  ago, still counted" behavior phase 4 exists to kill.
  `internal/buildinfo/contract.go`'s own history entry 3 names this
  trap ("a GUI built after it, talking to an older daemon, would show
  every session as permanently idle"). `Welcome.DaemonContract` already
  reaches `BuildModel`, so the discriminator cost nothing: `waiting()`
  falls back to `needs_attention` below contract 3 and reads `state`
  at or above it. Pinned by `TestBuildModelFallsBackToBellOnOldDaemon`
  and `TestBuildModelIgnoresBellWhenStateIsAvailable`.

- **2026-09-05 (phase 4)** — The notification wording is composed in
  `app/events.ts` (`fireBellNotification`), not in `cmd/hivegui/app.go`
  as the Files-to-change list says. `App.Notify` is a four-argument
  pass-through to `internal/notify`; it has no idea what a session is,
  and giving it one would have put a second state vocabulary in Go for
  no gain. Only `waiting_permission` gets its own body — everything
  else that reaches `needs_attention` (a bare bell on the heuristic
  tier) keeps "Waiting for input", which is still true for it.

- **2026-09-05 (phase 4)** — No `StateDot.tsx`, and no separate tooltip
  element: the tooltip is the existing `StateIcon`'s `<title>`, filled
  from a new `stateTooltip()` beside `sessionState()` in
  `lib/session-state.ts`. Same argument the phase-1 entry above makes
  for not adding the component — the icon already exists, already has
  the words channel, and a second surface for the same session could
  disagree with the first. The tier line ("reported by the agent" /
  "guessed from terminal output") is always present because it is what
  explains the uncertain ring to someone who hovered to ask about it;
  `hook` and `extension` collapse to one phrase, since which transport
  the agent used to speak is not the user's business.

- **2026-09-05 (phase 4)** — `verbS()` is deleted from
  `cmd/hivebar/model.go`. "N waiting on you" reads correctly at every
  count, so the helper that existed only to conjugate "N need/needs
  you" had no second caller.

- **2026-09-05 (phase 4)** — The minimized `Chip` does NOT get the new
  tooltip. It takes a resolved `SessionState` rather than a session, so
  feeding it one would have meant threading the carrier through a
  presentational component; its button already carries a `title`, and
  the success criterion names the sidebar row and the tile header.

- **2026-09-05 (phase 4, review iter 1)** — CORRECTS the phase-4 entry
  above, which justified the always-on tier line as "what explains the
  uncertain ring to someone who hovered to ask about it". There is no
  uncertain ring. Nothing in `theme/` or `Icon.tsx` reads
  `state_source`; the glyph is identical on every tier. The entry
  described an affordance the tree does not have — the same drift class
  the merge gate had just flagged in `control-plane.md`, reintroduced
  in the commit that fixed it. The tier line's real justification is
  that it is the ONLY surface carrying the `state_source` distinction,
  which is why it stays; `docs/design-docs/ui/icons.md` now says that
  instead.

- **2026-09-05 (phase 4, review iter 1)** — The tier line is omitted for
  `starting` and for a dead session's `exited`/`error`. It is a claim
  about where a state came from, and no tier produced those:
  `starting` is resolved client-side from `phase`, and death is
  observed from the process exiting. Appending "guessed from terminal
  output" to them fabricated a provenance — and two tests had pinned
  the fabrication as expected.

- **2026-09-05 (phase 4, review iter 1)** — `SOURCE_WORDS`, a map of
  the two tiers that exist, became `sourceWords()`, a truthiness test.
  Only the heuristic tier is spelled `""` on the wire, so a tier a
  future daemon adds is reported by definition; the map would have
  silently relabelled it a guess.

- **2026-09-05 (phase 4, review iter 1)** — The changeset's claim that
  "an agent that rang once and went back to work no longer counts
  against you" was withdrawn: it describes #338, not this PR.
  `internal/registry/registry.go:211` sets `NeedsAttention:
  needsAttention(st.State)`, and `needsAttention` (`:184`) is
  bit-for-bit the predicate `waiting()` now applies — so on any daemon
  at contract ≥ 3 the old count and the new one are identical, and
  below it `waiting()` returns `NeedsAttention` verbatim. The rename is
  still worth having (hivebar stops depending on a derivation it does
  not own, and the old-daemon fallback becomes explicit rather than
  accidental), but it is a decoupling, not a user-visible fix. The
  user-visible change is the wording alone. README carried the same
  misleading contrast and was trimmed with it.

- **2026-09-05 (phase 4, review iter 2)** — The GUI's collapsed project
  card said "k need(s) you" while the menu bar now said "N waiting on
  you", off the same predicate. Aligned on the menu bar's wording
  rather than the card's: it is the one the spec's success criterion
  names, and "waiting on you" needs no plural form, so the card's
  conjugation went away with it. `docs/design-docs/ui/components.md`
  and three test assertions moved with it. The divergence was this
  PR's own doing, which is why it is fixed here rather than deferred.

## Review log

- **2026-09-04** — `/hs-feature-plan-review` (three `hs-reviewer` passes:
  grounding, gaps, YAGNI). Changes:
  - Fixed `Entry.Info()` line (145, not 160); replaced the non-existent
    "restart file" with the real `Revive`/`Restart` sites in `registry.go`
    and required `SpawnArgs` there too (a restarted Claude session would
    otherwise silently lose its hooks).
  - Exit is observed in `watchSessionExit`; dropped `SetExitHook`.
  - Notifications are fired by the GUI (`app.go:113`), not the daemon;
    moved that work item.
  - Added the raw-`Cmd` branch (`create.go:511`) rule: env yes, args no.
  - Added `cmd/hivebar/client.go`+`model.go`, `test/e2e/wails-mock.ts`,
    `cmd/hivegui/app.go`, `README.md`, `.changesets/` to Files to change;
    named `hv-state-pulse` / `--motion-pulse` and `SessionRow.tsx:143`
    `title=` as the patterns to reuse.
  - Error paths made explicit: event-mode malformed JSON / read
    deadline; hook write deadline; `EnsurePiExtension` failure is
    non-fatal; `HivedPath` unresolvable ⇒ adapter skipped; state machine
    always non-nil so a pre-ready `AGENT_EVENT` applies.
  - YAGNI cuts: `HIVE_PROJECT_ID` (to 337), session id out of
    `SpawnArgs`, `SetExitHook`.
  - Tests added for each of the above; `## Verification` backfilled.
  - Open questions 2 and 3 resolved into Decisions; 1 stays as the
    phase-2 gate.

## Progress

- **2026-09-04** — Spec and plan written; stage PLAN.
- **2026-09-04** — Plan reviewed; open question 1 answered by experiment (hooks concatenate, Claude 2.1.260). Ready for `/hs-feature-plan-handoff 336`.
- **2026-09-04** — Stage → IMPLEMENT.
- **2026-09-04** — Phase 1 implemented on `feature/336-session-state-model`:
  `internal/agentstate/` (machine + exhaustive table tests), the four
  `SessionInfo` state fields and `SESSION_EVENT(state)`,
  `session.SetOutputHook`, the registry's per-chunk/bell/exit feeders
  and its 500 ms quiet ticker, `DaemonContract` 2 → 3, and the two new
  GUI state glyphs across sidebar / minimized chip / tile header.
  Verified: `go build ./...`, `go vet ./...`, `staticcheck` and
  `go vet` for darwin/linux/windows, `go test -race` on
  registry/agentstate/wire/session, `scripts/test.sh` (go · unit · dom ·
  e2e), `biome ci .`, `npm run typecheck`, and the full Playwright mock
  suite including the new `state-glyphs.spec`.
  Phases 2–4 (the `event` wire mode + `hived hook` + Claude adapter; the
  Pi extension; hivebar and notification wording) are untouched.
- **2026-09-04** — Smoke-tested in an iso build; three bugs found and
  fixed (see the Decision log): agents pinned to `working` and their
  bells swallowed (measured `ESC[?6n` polling), sidebar and tile
  disagreeing on state, and tile window titles missing.
- **2026-09-04** — Second smoke test rejected the descope: an agent with
  no state at all is not an acceptable answer. The heuristic tier now
  samples a screen digest instead of byte arrival, which covers every
  session, and the end-to-end PTY tests that should have caught all of
  this in the first place are in place.
- **2026-09-04** — Phase 2 implemented on `feature/336-session-state-model`:
  `wire.ModeEvent` + `wire.AgentEvent`/`AgentEventKinds` +
  `wire.FrameAgentEvent` (0x22), `DaemonContract` 3 → 4; the daemon's
  `ModeEvent` HELLO arm (`serveEvent`: one frame, 2s read deadline,
  kind/source validated, text truncated to `MaxSummaryLen`, closes with
  no reply); `Registry.ApplyAgentEvent`, `SetSocketPath`/`SetHivedPath`,
  and `HIVE_SESSION_ID`/`HIVE_SOCKET` injected into every spawned
  session's env on create, Revive (boot path) and Restart; `Def.SpawnArgs`
  + `SpawnInfo` on `internal/agent.Def`, wired for Claude only
  (`claude.go`: `--settings` hooks JSON built with `encoding/json`,
  shell-quoted `hivedPath`, a `sync.Once` version gate at
  `minHooksVersion = "2.1.0"`); `cmd/hived/hook.go` (`hived hook`
  subcommand, dispatched before `flag.Parse`, tolerant JSON mapping of
  every Claude hook event Hive wires, 2s dial/write deadlines, always
  exits 0, never writes stdout). Deviations from the plan, both because
  the referenced code didn't exist as described: there is no literal
  "raw-Cmd branch" file/line in `create.go` — the equivalent is
  `resolveAgentCmd`'s early return when `spec.Cmd` is already set, which
  is where the "env yes, SpawnArgs no" rule actually lives; and
  `maxKnownBadHooksVersion` is implemented but currently unset (no known
  bad release yet) rather than carrying a real value. `Notification`'s
  and `UserPromptSubmit`'s exact hook-payload field names could not be
  confirmed from a fetchable, literal JSON example in Anthropic's docs
  at write time, so `hived hook` reads `prompt`/`user_message`/`message`
  and `error_type`/`error`/`reason` as a fallback chain instead of a
  single hard-coded key — `scripts/probe-claude.sh` (not yet written;
  deferred, see below) is what would catch a wrong guess against a real
  `claude` binary. The Pi extension, `hivebar`, and GUI/notification
  wording (phases 3–4) are untouched, as phased.
  Verified: `go build ./...`, `go vet ./...` (host, `GOOS=linux`,
  `GOOS=windows`), `go test -race ./internal/... ./cmd/hived/...`
  (including new daemon `TestEventMode*`, registry
  `TestSpawnEnvCarriesHiveIDs`/`TestRawCmdGetsEnvNotSpawnArgs`/
  `TestCreateAppendsSpawnArgsForResolvedAgent`/`TestRestartAppendsSpawnArgs`/
  `TestReviveStartsIdleHeuristicEmptyText`/`TestAgentEventBeforeReadyApplied`,
  agent `TestClaudeSettingsJSONQuotesPath` and the version-gate tests,
  and `cmd/hived`'s `TestHookMapsEveryEvent` fixture table plus two
  integration tests driving a real daemon end-to-end through every hook
  fixture). Not done in this pass: `scripts/probe-claude.sh` (the
  real-`claude` drift probe) and a manual checklist run against an
  actual `claude` binary — see the open item below.

- **2026-09-04** — Solidification pass after the phase-1 churn:
  transition log (`HIVE_DEBUG_STATE=1`), frozen transition table, GUI
  attention set deleted (daemon `needs_attention` is the only owner),
  recorded PTY fixtures replayed through VT + machine in CI
  (`internal/session/state_fixture_test.go`), phase 2 hook tier, and
  the live probe `TestClaudeProbeWaitingPermission`. Probe transcript on
  claude 2.1.261 (checklist rows 7–12):

  ```
  idle -> idle               src=hook reason=ping                 (SessionStart)
  idle -> working            src=hook reason=prompt               (UserPromptSubmit, 2 s after typing)
  working -> waiting_permission src=hook reason=waiting_permission (PermissionRequest, 3 s)
  waiting_permission -> exited  src=hook reason=session_end        (/exit)
  working -> idle            src=hook reason=turn_end             (Stop, earlier run)
  idle -> waiting_input      src=hook reason=waiting_input        (idle_prompt, 60 s after Stop)
  ```

  Two findings worth knowing: (1) with the user's Claude default set to
  auto mode nothing ever prompts, so `waiting_permission` cannot be
  observed without `--permission-mode default` — that is Claude config,
  not a Hive bug; (2) a bare-PTY recording of an idle claude contains
  zero bytes even when DSR/DA1 queries are answered, so the "ESC[?6n
  every 200 ms" measurement behind the screen digest only reproduces
  with the GUI attached. The digest is correct either way; the fixture
  test guards the byte-free case and `TestAgentTUIStateFlow` the other.
  Rows 1–6 and 13–14 of the checklist remain a manual pass in the iso
  build.

- **2026-09-05 (phase 3)** — Pi extension implemented on
  `feature/336-phase3-pi-extension` (branched off `main` after #338
  squash-merged; the old `feature/336-session-state-model` branch is
  dead). Shipped: `internal/agent/pi/hive.ts` (the extension),
  `internal/agent/pi.go` (`//go:embed` of it, `EnsurePiExtension`,
  `piSpawnArgs`), `SpawnArgs: piSpawnArgs` on the `IDPi` catalog def,
  and the `agent.EnsurePiExtension(stateDir)` call in
  `cmd/hived/main.go` immediately after `agent.SetCustomDir`. No wire,
  daemon, or registry change was needed: phase 2 already built the
  `event` mode, the `extension` source allowlist entry, `SpawnInfo`
  carrying `StateDir`, and `appendSpawnArgs` on every create/revive/
  restart path.
  Deviations from the plan, both verified against the installed pi
  0.85.0 `docs/extensions.md` rather than assumed: the `?`-suffix
  `waiting_input` heuristic is gone, replaced by the real
  `ui_prompt_start`/`ui_prompt_end` events (see the decision-log entry
  below); and `lastAssistantText` walks
  `ctx.sessionManager.getBranch()` entries (`{type:"message",
  message:{role, content}}`) rather than the `ctx.messages` /
  `event.messages` shape the plan sketched, which does not exist on
  `agent_settled`.
  Verified: `go build ./...`, `go vet ./...` (host, `GOOS=linux`,
  `GOOS=windows`), `go test -race ./internal/... ./cmd/hived/...` all
  clean, including the new `internal/agent` tests —
  `TestPiDefUsesSpawnArgs`,
  `TestEnsurePiExtensionWritesAtomicallyAndOnlyWhenStale`,
  `TestEnsurePiExtensionIgnoresEmptyStateDir`, `TestPiSpawnArgs`,
  `TestPiExtensionFramesAreValidWireFrames` (drives real `node` to run
  the extension's own encoder and decodes both frames with
  `wire.ReadFrame`), `TestPiExtensionKindsAreOnTheAllowlist`, and
  `TestPiExtensionRunsNodeTests` (`node --test pi/hive.test.ts`, four
  TS cases: inert without env, the exact subscribed event set, byte-not-
  character truncation at `MaxSummaryLen`, and the session-format walk).
  Both node-dependent tests skip cleanly when no TS-capable node is
  present (see the CI correction below).
  Not done in this pass: checklist row 14 as a manual pass in an iso
  build against a real `pi`, and `scripts/probe-claude.sh` (still
  outstanding from phase 2). Phase 4 (hivebar + notification wording +
  tooltip polish) is untouched, as phased.

- **2026-09-05 (phase 3)** — `daemon-contract-override` is claimed for
  this PR rather than bumping `buildinfo.DaemonContract` 4 → 5. The
  diff touches `cmd/hived/main.go`, which the gate watches, but a GUI
  built from this tree drives a daemon built without it perfectly: no
  frame, no registry field, and no session semantic changed. The only
  difference an old daemon shows is that Pi sessions stay on the
  heuristic tier — a feature that is absent, not a mixing hazard. A
  bump would cost every user their running agents to ship it.

- **2026-09-05 (phase 3, review iter 1 fixes)** — Applied on PR #341:
  the CI node bump and capability guard above; `ui_prompt_end` now posts
  `turn_end` when no turn is in flight (`turnInFlight`, set on
  `agent_start`, cleared on `agent_settled`) instead of always posting
  `permission_resolved`, which the machine reads as `working` and would
  have stranded a session there until `HookStaleAfter` for any blocking
  prompt raised outside a turn — an extension slash command, or a
  `confirm()` after `agent_settled`; `truncate` walks back over UTF-8
  continuation bytes rather than stripping a trailing replacement char,
  so a U+FFFD the user really typed survives, matching the Go side's
  `strings.ToValidUTF8`; and the kind-allowlist test strips `//`
  comments before scanning, since the extension names every kind in
  prose and the test would otherwise pass on a kind that survives only
  in a comment. Three new TS cases cover the two `ui_prompt_end`
  branches over a real unix socket (the extension's own encoder, a real
  connection) and the U+FFFD-at-the-cut case. The two socket-backed
  cases skip on Windows (`node:net` cannot bind a unix socket at a
  temp path there: `listen EACCES`), matching how the daemon's own
  event-mode tests already skip — the whole event tier is unix-only,
  since `hived hook` dials `net.Dial("unix", ...)` too.


- **2026-09-05 (phase 3, review iter 2 fixes)** — Applied on PR #341:
  the `session_shutdown` reason gate and the `Machine.Apply` ordering
  guard above, plus `EnsurePiExtension`'s file modes tightened from
  `0o755`/`0o644` to `0o700`/`0o600` to match every other writer under
  `StateDir()` (`registry/persist.go`, `registry/logfile.go`). New
  tests: `internal/agentstate` gains `TestApplyDropsOutOfOrderEvents`,
  `TestApplyAcceptsEqualTimestamps` and
  `TestApplyFirstEventNeverDropped` (the zero-`hookSeenAt` boundary),
  and the TS suite gains a table case asserting the reported kind for
  all five shutdown reasons plus the missing-reason fallback.
  Verified: `go vet` (host, linux, windows), `scripts/test.sh go` green,
  `node --test internal/agent/pi/hive.test.ts` 8/8.


- **2026-09-05 (phase 3, review iter 3 fixes)** — Applied on PR #341:
  the `at`-stamp hoist and the `ApplyAgentEvent` clamp above, plus
  `README.md`'s toolchain line corrected to Node 24+ (it still said 20+
  after this PR moved CI's pin, so a contributor following it would
  silently skip the only cross-language wire-frame check). New test:
  `TestApplyAgentEventClampsFutureStamp` — a 24-hour-future event
  followed by a correctly stamped one, asserting the second still
  applies. Deliberately not taken from iter 3's MINORs:
  `lastAssistantText` re-walking the branch per settle (a short list
  walked at human speed; caching it would add invalidation for no
  measurable gain) and `piExtensionWarnOnce` being a package global
  (it mirrors `claudeHivedPathWarnOnce` exactly — changing one adapter's
  shape and not the other's costs more in consistency than it buys in
  testability).


- **2026-09-05 (phase 3, verification note)** — Every node subprocess
  the tests spawn is now bounded (`exec.CommandContext`, 90 s / 60 s /
  30 s): the capability probe, the frame-encoder run, and
  `node --test`. Why: `TestPiExtensionRunsNodeTests` shelled out with
  `exec.Command(...).CombinedOutput()` and no deadline, so a node that
  failed to exit would block until the package timeout and surface as
  "the package hung", naming no test. Found while chasing a local
  timeout that turned out to be something else entirely — the bound is
  worth having regardless, since a hang that reports itself as a hang
  costs one line and saves an investigation.
  On the local timeout itself, for the next person who hits it: the
  full suite fails on a loaded machine with every in-flight package
  killed at exactly 180 s, including packages that finish in under a
  second. That number is not `scripts/test.sh`'s `-timeout 120s`; it is
  cmd/go's own kill timer, which fires at timeout + 60 s when a test
  binary does not die on its own. `go test ./... -p 1` passes the whole
  suite in ~85 s on the same tree, and CI passes it in parallel, so it
  is contention on the developer machine (Hive's suite spawns many PTYs
  and daemons, and a dev box is usually already running real `hived`
  instances), not a defect. Reach for `-p 1` before believing a
  wholesale local red.


- **2026-09-05 (phase 3, review iter 4 fixes)** — Applied on PR #341:
  the bounded ordering window above; TS coverage for the two untested
  behaviours the tier is built on — `ui_prompt_start`'s
  confirm/select → `waiting_permission` vs everything else →
  `waiting_input` mapping (one of this PR's two documented deviations
  from the plan, previously assertable only by reading the source) and
  the `input` `source !== "extension"` filter; the Node 20 → 24 sweep
  finished in `CONTRIBUTING.md` and `build.sh` (three files had stated
  three different floors), with the `go` test-layer row now noting that
  it shells out to node; `EnsurePiExtension` moved after the `hived.log`
  tee in `cmd/hived/main.go`, since the GUI-spawned daemon has
  stdout/stderr on /dev/null and that line is the only explanation a
  user gets for Pi staying on the heuristic tier; the idempotence
  assertion switched from `ModTime` equality to `os.SameFile`, which
  sees the temp+rename inode swap exactly; the kind-allowlist test now
  scrapes kinds out of `post(...)` calls instead of walking a hardcoded
  list (it caught `"confirm"`/`"select"` from the ternary's comparison
  operands on the first run, which is the sort of thing a hardcoded list
  can never catch); and `ApplyAgentEvent` samples `time.Now()` under the
  registry lock, since for the empty-`at` fallback that clock *is* the
  ordering key.
  Verified: `go vet` (host, linux, windows); `./internal/agent`,
  `./internal/agentstate`, `./internal/registry`, `./internal/daemon`
  green; `node --test internal/agent/pi/hive.test.ts` 10/10.


- **2026-09-05 (phase 3, review iter 5 fixes)** — Applied on PR #341:
  the ordering guard's boundary is now pinned on both sides
  (`TestApplyOrderingBoundary`: exactly `HookStaleAfter` behind must
  apply, one nanosecond inside the window must drop) — without them,
  flipping the guard's `<` to `<=` left the whole suite green, and the
  file already pins `trusted()`'s boundary the same way. The guard's
  comment no longer claims `Observe`/`Tick` recover every case: they
  recover `working` and `idle`, but `Observe` returns early on a wait by
  design and `Tick` only demotes `working`, so a report landing
  `HookStaleAfter` or more behind *on a wait* pins that wait until
  `ClearWaiting` or the next agent event. Reaching it needs a reporter
  stalled past the staleness horizon — the Pi extension destroys its
  connection after 2 s, so only a wedged `hived hook` qualifies — and it
  is left as-is rather than special-cased, since a state-dependent
  rewind rule is more machine than the case is worth. The kind-scrape
  test's doc comment now states its actual scope (string literals at the
  call site).
  Verified: `go vet` (host, linux, windows); `./internal/agent`,
  `./internal/agentstate`, `./internal/registry`, `./internal/daemon`
  green; `node --test internal/agent/pi/hive.test.ts` 10/10.


- **2026-09-05 (merge gate)** — Gate FAIL, run on the degraded post-merge
  path (PR #341 already merged as `d49551c`; range evaluated as the whole
  feature, `ad8f4aa~1..origin/main`). Failing checks:
  - Success criterion 7 (`hivebar` count off the new state) — not
    implemented; phase 4 work.
  - Success criterion 6, tooltip half (prompt + summary) — not
    implemented; phase 4 work. Glyph half passes.
  - `docs/design-docs/control-plane.md:137-144` documents
    `scripts/probe-claude.sh` and `testdata/claude-hooks/` as existing;
    neither is in the tree or in history.
  - `docs/design-docs/ui/icons.md:37` says the Pi extension "will in
    phase 3" after phase 3 shipped.
  - `docs/design-docs/control-plane.md:38` claims the hook tier learns
    "subagent activity"; no `Subagent*` handling exists.
  Non-goals dimension passed clean. Stage stays at GATE. No follow-up
  issues filed yet — the gate's failing criteria are the already-planned
  phase 4 plus a three-paragraph doc correction, not unplanned debt.

- **2026-09-05 (phase 4)** — hivebar, notification wording and the
  state tooltip. `cmd/hivebar/model.go`: `Model.Attention` →
  `Model.Waiting` and `SessionRow.NeedsAttention` → `SessionRow.Waiting`,
  both fed by one `waiting()` predicate so the summary line and the row
  markers cannot disagree; summary reads "N waiting on you".
  `lib/session-state.ts` gained `stateTooltip()`, wired into
  `StateIcon`'s new optional `detail` prop from `SessionRow.tsx` and
  `TileChrome.tsx`. `app/events.ts` names `waiting_permission` in the
  notification body. Docs: `control-plane.md` drift-probe paragraph now
  describes the probe that exists (`cmd/hived/claude_probe_test.go`,
  `HIVE_PROBE_CLAUDE=1`, fixtures at `cmd/hived/testdata/hooks/`)
  instead of the never-committed `scripts/probe-claude.sh`; the tier
  table drops the "subagent activity" claim (no `Subagent*` handling
  exists); `ui/icons.md` stops saying the Pi extension "will" report
  waiting_permission and documents the tooltip stack; README's menu-bar
  bullet says the dot means blocked-on-you, not rang.
  Verified: `go vet` host/linux/windows clean; `go test ./internal/...
  ./cmd/hived/ ./cmd/hivebar/` green; `scripts/check-daemon-contract.sh
  origin/main HEAD` → "no daemon-side changes" (so no contract bump);
  `biome ci .` clean; `tsc --noEmit` clean; vitest 1027/1027 in 88
  files; `CI=1 npx playwright test` 270 passed / 31 skipped.
  Not done in this pass: the manual smoke checklist in an iso build
  (rows 1–6 and 13–14 still unrun, and no row covers the menu bar).

- **2026-09-05 (phase 4, review converged)** — PR #344 converged on
  review iteration 2: APPROVE, zero unresolved threads, MERGEABLE.
  Iteration 1 returned COMMENT with three IMPORTANT findings, all of
  them prose asserting things the tree did not support (see the
  correction entries in the Decision log); iteration 2 verified each
  fix individually against the tree and found the diff's remaining doc
  claims — `control-plane.md`'s five named probe artefacts,
  `icons.md`'s Pi claim, the plan header's branch — all resolve to
  things that exist. Its one MINOR (menu-bar vs project-card wording)
  was fixed in the same pass.
  Verified after the wording alignment: `biome ci .` clean, `tsc
  --noEmit` clean, vitest 1029/1029 across 88 files, `CI=1 npx
  playwright test` 270 passed / 31 skipped, `go vet ./...` clean.
  Still not done, and not in phase 4's scope to fix: the manual smoke
  checklist in an iso build. Rows 1–6 and 13–14 have been outstanding
  since phase 2, and no row covers the menu bar at all — so the waiting
  count and the tooltip have never been seen in a running app.

- **2026-09-05 (merge gate, phase 4)** — Gate NEEDS_FOLLOWUP on the
  open PR #344 (pre-merge path; ledger converged at iter 2, APPROVE,
  zero threads). Items:
  - Criterion 2: `error` never observed on a real `claude`; checklist
    rows 1–6 and 13–14 still unrun in an iso build.
  - Criterion 3: no state ever observed from a real `pi` (row 14).
  - Criteria 6 and 7: implemented and unit-covered, but never seen in a
    running app — the checklist has no menu-bar row at all.
  - Criterion 5: implementation demotes at `HookStaleAfter = 30s` and
    only while the screen changes, not "within the quiet threshold" as
    the spec words it. Deliberate; the spec bullet is what is wrong.
  - `internal/agent/claude.go:111` cites `scripts/probe-claude.sh`,
    which has never existed.
  - Plan checklist row 14 (`:509`) instructs the operator to look for
    an "uncertain ring" that this plan refutes at `:948`.
  - No committed test drives `exited` through a real child exit.
  Non-goals passed clean. Stage stays at GATE.

## Open questions

<Empty — resolved into the Decision log.>

## PR convergence ledger

- **2026-09-04 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 2854c4ad3033402a11277eece2de916447c9861b2f3263fe8b2546f591750ec2; threads_open: 0; action: continue (2 IMPORTANT remain); head_sha: 4423148.
- **2026-09-04 iter 2** — verdict: COMMENT (strict); mergeable: MERGEABLE; findings_hash: bf94a2f0c66b9cec2198ab14123a12cac7906dff824e342940e56b9bab769543; threads_open: 0; action: escalated:risky fix needs human decision; head_sha: ac893ca.
- **2026-09-05 iter 1 (PR #341)** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 933fe7284759ec8b0b78ddf7426686dd96f3263ed89a5358a9ee055ef1aa3d69; threads_open: 0; action: escalated:risky fix needs human decision; head_sha: 73ec301.
- **2026-09-05 iter 2 (PR #341)** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: aeb45dd6f258cc7b432bc46e97db046988fdfc0417ce97805863f9762d5972c8; threads_open: 0; action: escalated:risky fix needs human decision; head_sha: 52310a7.
- **2026-09-05 iter 3 (PR #341)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: be5b1f45d0d6110670ca570825b18153cb3812780652253e88c0861f28d5277a; threads_open: 0; action: continue (3 IMPORTANT remain; loop's stop rule met but boil-the-lake says fix them); head_sha: 7c97502.
- **2026-09-05 iter 4 (PR #341)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 27e41405647bee014b40872c51634cc5e5605e836fb8f36b328c2543d025406d; threads_open: 0; action: continue (3 IMPORTANT + 5 MINOR fixed; stop rule met but one finding was a regression from iter 2); head_sha: 9592c67.
- **2026-09-05 iter 5 (PR #341)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 012bd1b0cb5b268652d7f421b16e0ddc5b99a330710aa60401cc653899411c66; threads_open: 0; action: converged (2 IMPORTANT fixed: boundary test + comment scope; reviewer re-derived iter 4's riskiest changes independently and found them correct); head_sha: d27c756.

## Gate verdict

- **2026-09-05** — verdict: NEEDS_FOLLOWUP; checks: 5 passed / 0 failed / 3 followups; followups: none filed (pending user decision); one-line: every criterion is implemented and unit-evidenced and all six non-goals hold, but four criteria have never been exercised against a real binary or a running app, criterion 5's fallback is not the one the spec words, and two stale prose references survive outside the docs that were swept.
  - 2026-09-05 dimensions:
    - acceptance — NEEDS_FOLLOWUP — criteria 1, 4, 6, 7, 8 PASS with executed evidence (`TestBuildModelCountsWaiting`/`IgnoresBellWhenStateIsAvailable`/`FallsBackToBellOnOldDaemon`; tooltip text asserted verbatim at `test/dom/attention-icon.test.tsx:135`; `TestMetaFileUnchangedByState`; real-PTY working/idle/bell). Criterion 2 followup: the recorded manual transcript covers rows 7–12 on claude 2.1.261 but never observed `error`, and `TestClaudeProbe*` skip without a real binary. Criterion 3 followup: no state ever observed from a real `pi` (row 14 unrun). Criterion 5 deviation: spec says fallback "within the quiet threshold", implementation demotes at `HookStaleAfter = 30s` (`internal/agentstate/machine.go:48`) and only while the screen keeps changing — a hooked session whose hook dies with a static screen holds its last hook state indefinitely; deliberate and recorded in the Approach, but not the spec's wording. Coverage gap: no committed test drives `exited` through a real child exit (`registry.go:1113`); a throwaway PTY test proved the path works and was deleted.
    - non-goals — PASS — all six hold after phase 4. The one at risk was the bell: `events.ts:249-261` branches the notification body only on `waiting_permission` and falls through to the original wording for bare-bell sessions (pinned by `events-focus.test.ts:284`), and hivebar's switch to the state predicate cannot drop bell sessions because a bell sets `waiting_input` and `waiting()` falls back below contract 3.
    - doc accuracy — NEEDS_FOLLOWUP — all three original gate failures and all three review-iteration-1 prose defects verified genuinely fixed by executed commands (every artefact `control-plane.md` now names exists; no `Subagent*` in Go; nothing under `src/theme/` or `Icon.tsx` reads `state_source`, so the tier line's replacement justification is true). Two residuals: `internal/agent/claude.go:111` still cites `scripts/probe-claude.sh` as an existing verification source (pre-existing from #338, the same defect class phase 4 swept from the docs but not from Go comments), and the plan's own smoke checklist row 14 (`:509`) still tells the operator to expect "the uncertain ring", which this file refutes at `:948`. Rows 13–14 are outstanding work, so that is live wrong guidance.

- **2026-09-05** — verdict: FAIL; checks: 11 passed / 3 failed / 2 followups; followups: none filed (pending user decision); one-line: phases 1–3 are delivered and evidenced, but two of the spec's eight success criteria (hivebar count, prompt+summary tooltip) are phase-4 work that was never implemented, and three design-doc paragraphs describe unshipped or already-shipped things in the wrong tense.
  - 2026-09-05 dimensions:
    - acceptance — FAIL — criterion 7 ("`hivebar` shows 'N waiting on you' using the new state, not the bell") not delivered: `cmd/hivebar/model.go:53,97,104,195` build rows solely from `wire.SessionInfo.NeedsAttention`, and the string appears nowhere in the tree. Criterion 6 partially delivered: glyphs ship and are covered (`test/e2e/state-glyphs.spec.ts`, 35 unit tests), but the tooltip half is absent — `last_prompt`/`last_summary` exist only as optional type fields at `cmd/hivegui/frontend/src/app/state.ts:72-73` and are read by zero components. Criteria 1, 4, 5, 8 PASS with green tests; 2 and 3 NEEDS_FOLLOWUP on their manual halves only (checklist rows 1–6 and 13–14 unrun in an iso build; `TestAgentTUIStateFlow` skips by default).
    - non-goals — PASS — all six negative checks clean. Strongest evidence on "no persisted state": `persist.go`/`closed.go`/`store.go` have zero diff in `ad8f4aa~1..origin/main`, `TestMetaFileUnchangedByState` byte-compares `session.json` across a full state tour, and a runtime enumeration of `agent.All()` confirms `SpawnArgs` is non-nil only for `claude` and `pi`.
    - doc accuracy — FAIL — `docs/design-docs/control-plane.md:137-144` states in the present tense that `scripts/probe-claude.sh` exists, runs in CI and is on the release checklist, and that fixtures live under `testdata/claude-hooks/`; neither exists in the tree or anywhere in history. `docs/design-docs/ui/icons.md:37` still says the Pi extension "will in phase 3" after phase 3 merged as `d49551c`. `docs/design-docs/control-plane.md:38` claims the hook tier learns "subagent activity"; no `Subagent*` handling exists in Go. Changesets (5), CHANGELOG generation, README, and the plan header/Progress log all PASS.
- **2026-09-05 iter 1 (PR #344)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: b530c88feba83824bccf91b3c0e9765522fe74e8635811954d5b27a4a2b83238; threads_open: 0; action: continue (stop rule met — COMMENT, strict off, zero threads — but 3 IMPORTANT prose findings stood and boil-the-lake says fix them); head_sha: 8abfa67.
- **2026-09-05 iter 2 (PR #344)** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop (converged; iter 1's three IMPORTANT findings each re-verified as fixed, one MINOR wording divergence fixed in the same pass); head_sha: 20f21f5.
