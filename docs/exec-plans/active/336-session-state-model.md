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
  `--motion-pulse` for the waiting states. `title` attribute /
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

Phase 2 additionally: `HIVE_PROBE_CLAUDE=1 scripts/probe-claude.sh`
(skips cleanly when `claude` is not on PATH) and the manual checklist
above. Phase 3: `node --test internal/agent/pi/` when `node` is present.

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
- **2026-09-04 (phase 1, after smoke test)** — The heuristic tier
  applies to shell sessions only (`Entry.Agent == ""`); agents keep
  exactly their pre-336 behaviour until their hook/extension tier
  lands. Why: measured on a live session, an idle Claude Code emits an
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
- **2026-09-04 (phase 1, after smoke test)** — `STATE_WORDS.running`
  reverts to "Running". Why: with agents off the heuristic tier, an
  empty `state` means "no claim", not "idle", and an agent session
  would otherwise be labelled with a fact nothing established.
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
  disagreeing on state, and tile window titles missing. Phase 1's
  user-visible scope is now shell sessions.

## Open questions

<Empty — resolved into the Decision log.>
