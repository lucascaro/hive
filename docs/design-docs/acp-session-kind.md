# ACP session kind: research notes (spec 496)

This note is the research for [spec 496](../product-specs/496-add-an-acp-session-kind-transcript-view-exact-stat.md). It covers the code an `acp` session kind touches and the findings from spike 492 that shape it. The design rationale is in [acp-workflows.md](acp-workflows.md). Line numbers were taken on 2026-10-03 against main `e68e1c88`.

## Daemon

### Spawn path and persistence

- `Registry.Create` is at `internal/registry/create.go:96`. It runs `beginCreate` (:112), then `finishCreate` (:148), then `finishCreateTail` (:168). The PTY spawn itself happens at create.go:177, through the `startSession` seam in `registry.go:41-72`.
- `attachSession` (create.go:1327) binds `e.sess` and pins `AgentSessionID`.
- `insertEntry` (create.go:600-653) persists the entry *before* the spawn.
- Metadata lives in `Entry` (`registry.go:91-187`). Its on-disk mirror is `MetaFile` (`persist.go:12-41`, every field `omitempty`). Writes are atomic (`writeAtomic`, persist.go:92).
- A new `Kind` field has to be threaded through all of these:
  - `Entry` and `MetaFile`;
  - `persistEntryLocked` (registry.go:1953);
  - both `load()` literals (registry.go:1193 and :1216);
  - `insertEntry` (create.go:611);
  - `Tombstone.Meta` and `Restore` (closed.go:111-118, :361);
  - `Entry.Info()` (registry.go:271);
  - `wire.SessionInfo` (control.go:163) and `wire.CreateSpec` (control.go:51).

  An empty `Kind` decodes as `pty`, so state written before this change still loads.

### Revive and Restart

- `reviveAll` (daemon.go:488) runs `revivePass`, then `ReviveWithPhase` (registry.go:1265), then `Revive` (registry.go:1315). `Revive` uses `ResumeArgs(AgentSessionID, cwd)` when it has an id. ACP entries must branch here into a `session/load` path, and so must `Restart` (registry.go:1422-1509) and `Restore`. Otherwise boot would fork a PTY for them.
- `Restart` is the template for takeover:
  1. set the phase to restarting;
  2. close the old child and wait on `Done()`;
  3. clear `e.sess` only if it still holds the same child, so `watchSessionExit` does nothing;
  4. start the resume command;
  5. set the phase to ready.

### Coupling to `internal/session`

- `session.Session` (session.go:35) is a concrete type and PTY-only. The registry calls it at about 50 sites (`e.sess`). The daemon uses it only in `serveAttach` (daemon.go:1891-1990).
- `Alive()` is defined as `e.sess != nil` (registry.go:265).
- Recommendation from research: don't extract an interface. Add a separate `e.acp` child, and widen `Alive()`, `Kill` (:1731), `Close` (:1901), `Restart`, `watchSessionExit` (:1516), `ResolvePrompt` (:712) and `title()` to cover it. An interface would be mostly stubs, because ACP has no screen, resize or replay.
- `serveAttach` must refuse an ACP session with a distinct error. Today it would report `session_dead`.

### State tiers

- The source constants are in `wire/control.go:327-347`: heuristic is `""`, plus `hook`, `extension` and `laya`.
- The state machine is `internal/agentstate/machine.go`:
  - `trusted()` (:274) means a non-heuristic source seen within `HookStaleAfter`, which is 30s (:58);
  - `Tick` (:452) demotes a stale tier;
  - `Apply` (:477) promotes.
- The daemon accepts external events only from `hook` and `extension` (daemon.go:962).
- ACP events should be fed to `Apply` in-process, with `Source: "acp"`.
- The `acp` tier must not go stale: a turn can be silent for more than 30s, and the ACP stream is authoritative until it closes.

### Wire

- The last frame is `FrameSetClientUI = 0x3e` (frame.go). Each new frame has to be:
  - classified in `ControlRequestFrames`, `ControlEventFrames` or `NonControlFrames` (checked by `TestEveryFrameClassified`);
  - mapped in `controlEvents` (client.go:119-139), for server-push frames;
  - given a row in `docs/plugins.md`, which a parity test checks.
- Dispatch happens in `handleControlFrame` (daemon.go:1446). The template is `FrameGetActivity` (:1499), and long-running work goes through `runOp`.
- `sessionModeFrames` (:1315) denies new verbs to session-mode callers by default.
- Clients that must change in lock-step:
  - `cmd/hivegui/app_calls.go`
  - `cmd/hived-ws-bridge/main.go`
  - `internal/wire/testclient`
- `buildinfo.DaemonContract` is 21. This change needs a bump.

### Child processes

- `proc.CommandContext` is the only way to spawn a child (`TestNoDirectExecOnWindows`).
- The plugin manager (`internal/plugin/manager.go:640-695`) is the template for a supervised long-lived stdio child:
  - `resolveCommand`;
  - a scrubbed env;
  - `WaitDelay`;
  - own process group plus `proc.KillTree` (`internal/proc/tree_unix.go`, moved from the plugin package by 496).
- ACP is the first bidirectional JSON-RPC child in the codebase.

### Agent catalog and settings

- `agent.Def` (agent.go:29-113) has `ResumeArgs`, `SessionIDFlag`, `CaptureSessionIDFn`, `SpawnArgs` and `SpawnEnv`.
- The Pi extension is `EnsurePiExtension` (pi.go:38).
- **Hive has no per-agent permission or unattended setting.** `agent.Settings` (settings.go:40-81) holds only task-tool toggles, Laya options and plan-review options. Spec criterion 5 needs one designed from scratch.
- **Hive has no Node detection.** PTY agents run under `$SHELL -l -i -c`, so nvm/fnm PATHs apply to them. The daemon's own PATH, and the PATH of a GUI launched from Finder, usually lacks `npx`. Running an adapter under an interactive shell over pipes is unsafe: rc output corrupts the JSON-RPC stream. So resolve the login-shell PATH once, and exec `npx` directly with it.

### Provenance

- `SpawnedBy` exists only in docs: acp-workflows.md:227 and plan 391. Plan 391 is not started; it waits on 389 and 390. It says the daemon stamps `SpawnedBy` and the client never sends it.
- 496 is the first change to need the field.

## GUI

### Tiles

- Each tile is a `SessionTerm` (`session-term.ts:184`), built by `ensureTerm(info)` (:1872). That factory is injected into `view.ts`/`grid-layout.ts`.
- `TermTile` (`app/state.ts:254-334`) is the tile contract, with about 25 members. About 37 call sites reach tiles through `termsMap()`/`getTerm()`.
- An `AcpTile` implementing `TermTile` without `term` keeps the grid, minimize and `TileChrome` working.
- `focus.ts:157,229-241,314` hard-codes `.xterm-helper-textarea` as the focus target, so the ACP tile needs its own focus target.

### Launcher

- `Launcher.tsx` has its own hand-coded key listener (:359-450). `launchSelected` (:302) calls `CreateSession`.
- The worktree toggle (:567-604) is the precedent for a toggle that can be disabled with a visible reason.
- `AgentInfo` (`app_calls.go:32-46`) would gain the ACP availability fields.

### State display

- `sessionState` already maps `waiting_permission`.
- `isInferredSource` treats an unknown source as reported, so `acp` renders as authoritative with no change.

### Streaming

- `store/activity.ts` is the snapshot-plus-delta template.
- Server-push events arrive through `controlReadLoop`, which calls `EventsEmit` and then `events.ts`.
- The names `transcript:*` and `SearchTranscript` are taken by find-in-session (spec 431).

### Reusable components

- `Markdown` is XSS-safe. Use it for agent text.
- `components/activity/` already renders plan and tool rows.
- There is no textarea primitive yet.
- UI rules (`docs/design-docs/ui/README.md`):
  - tokens only;
  - sprite icons only;
  - a mock in `ui/mocks/` for any new visual choice;
  - `scripts/ui-lint.sh --strict` must pass.

### Commands and keys

- `restart-session` is the precedent for a palette and menu command with no default key.
- The ACP prompt box needs a `text-input` key scope, like `findBox`.
- Inline Allow/Deny keys need a `matched` scope, plus a `FIXTURES` entry in `every-shortcut.spec.ts`.

### Tests

- The mock bridge is `test/e2e/wails-mock.ts`.
- `bridge-harness-parity.test.ts` requires every bridge export to exist in both the mock and `e2e-real/wails-bridge.ts`.

## Spike 492 facts the implementation must honour

- **Adapters**, pinned through `npx -y`:
  - `@agentclientprotocol/claude-agent-acp@0.85.1`
  - `@agentclientprotocol/codex-acp@2.1.1`
  - `pi-acp@0.0.34`
  - Gemini (`gemini --acp`) and Copilot (`copilot --acp --stdio`) are native but unprobed.
- **Methods proven:** `initialize`, `session/new`, `session/prompt`, `session/load` and `session/request_permission`.
- **Methods not proven:** `session/cancel`, `session/set_mode` and set-config.
- **Message shapes** are in `scripts/acp-probe/probe.mjs:152-184` and `:381-473`, and `testdata/fake-agent.mjs`. No real transcript is committed, so Go golden fixtures come from the fake agent plus one raw capture.
- **`session/load` replays history** as `user_message_chunk` and `agent_message_chunk`. That is how the transcript survives a restart without Hive storing it, although Hive may cache it as well.
- **The MCP servers must be re-sent on `session/load`.** The probe sent `[]`.
- **Permission identity:**
  - Claude puts `mcp__<server>__submit_result` in the request's `name` (F7).
  - Match on the exact structured identity, bound to that session's own server name, and fail closed when the identity is missing (`probe.test.mjs:124-147`).
- **F2.** In a PTY, Codex leaves a detached `codex app-server` (ppid 1, its own process group) that holds the thread's writer lock. No release mechanism is known. Hand-back must detect the lock and fail with a clear error, or release it.
- **F3.** Codex skipped `submit_result` in 1 of 5 runs. A missing result counts as a failure, never a success.
- **F4.** Codex's default mode `agent` raised no permission request, so the mode must always be set explicitly.
- **F5.** Pi ignores `mcpServers` and has no approval gate. Its `submit_result` comes from Hive's Pi extension.
- **F6.** Takeover means:
  1. stop the ACP child;
  2. run `ResumeArgs` in a PTY;
  3. on hand-back, close the PTY and run `session/load`.

  The PTY-to-ACP direction was never probed.
- **Environment.** Strip `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION` and `HIVE_SOCKET` from adapter children, or Claude stops saving transcripts. Keep user preferences such as `CLAUDE_CODE_ENABLE_TODO_TOOLS`.
- **Go SDK.** There is no official one. `coder/acp-go-sdk` trails the schema. A small hand-rolled newline-delimited JSON-RPC client, like the probe's `Rpc`, is the expected path.

## What phase 1 built

- `internal/acp`: the JSON-RPC client (`conn.go`), the adapter process (`agent.go`: own process group, `AdapterEnv`, login-shell PATH, a stderr tail), and `Transcript`.
  - Message chunks coalesce, and tool updates merge by id.
  - Live user echoes are ignored, because `PromptACP` records the turn with its origin. During a `session/load` replay those echoes *are* the record.
- `internal/acp/acptest`: a Go fake agent that tests re-exec. It keeps one JSONL history per conversation so a second process can `session/load` it, and logs every call to `calls.log`.
- **Registry** (`internal/registry/acp.go`): `Entry.acp` sits beside `Entry.sess`.
  - Every revive path (boot, `Restart`, `Restore`) goes through `Revive`, which branches to `startACP(..., loadID)`.
  - Prompts are one at a time; a second is refused with `ErrACPBusy`.
  - A permission request waits for `AnswerPermission`, and the answer must name the pending request's id and one of the options it offered.
- **State** comes in-process with `Source: acp`. The tier has no staleness deadline, and hook or extension events for an ACP entry are dropped.
- **Wire:**
  - frames `0x3f`–`0x42`;
  - `SessionInfo.kind` and `spawned_by`;
  - `CreateSpec.kind`;
  - `CreateSpec.SpawnedBy` is `json:"-"` and is stamped by the daemon from the connection.
- **Attach.** Attaching to an ACP session gets `ErrCodeACPSession`. A create HELLO with `kind: acp` is refused, because it would turn into an attach.
- **Review-round hardening.**
  - The transcript is budgeted by its JSON-escaped size, so a snapshot always fits one frame (`wire.MaxPayload`).
  - Streamed chunks go out as `append` deltas.
  - A `session/load` replay is broadcast once, as a reset, when it ends. Broadcasting it per chunk could overflow a listener mid-replay.
- **GUI staleness.** `isStale` needs no change: the daemon sends no `stale_at` for the `acp` tier.

## What phase 2 built

- **Tile.** An ACP session gets an ordinary grid tile. `session-term.ts` marks its host `.acp` and never attaches: `ensureAttached` returns `deferred` for the kind, and `setInfo` attaches if the kind ever flips back to `pty`. `TileChrome.tsx` portals `components/acp/AcpTranscript.tsx` into the tile's overlay host, and `acp.css` hides the terminal body under it.
- **Store.** `src/store/acp.ts` holds one transcript per session, fed by `acp:transcript`. The fold is pure (`src/lib/acp.ts`):
  - a reset replaces everything;
  - a delta before the first snapshot is dropped, since the snapshot that follows already holds it;
  - a delta from another epoch marks the copy stale, and the view refetches;
  - `append` deltas add text to the end of their item;
  - every message overwrites the pending permission.
- **Snapshot ordering (contract 23).** Phase 1 wrote the `GET_ACP_TRANSCRIPT` reply directly while deltas went through the connection's fan-out goroutine, so a reply could overtake or trail a delta and the view would lose or double a chunk. `Registry.SendAcpTranscript` now queues the snapshot on the caller's own listener under `r.mu`, which orders it with the deltas. That is what lets the client fold in plain arrival order.
- **Launcher.** A Terminal/ACP radio pair, Terminal by default on every opening. It is offered only for a plain new session (no duplicate, opening prompt or continue). In ACP mode a row that cannot run it is disabled with `AgentInfo.acpReason` (`agent.Def.ACPAvailable`, judged on the GUI's login PATH — Q11), and unprobed adapters carry an `experimental` tag.
- **Keyboard.** Focusing an ACP tile focuses its prompt box. The `acp-prompt` key scope (text-input) gives it plain typing; Enter sends and Shift+Enter is a newline. The permission buttons have no key: they are ordinary buttons reached by Tab or a click, and answering one hands focus back to the prompt box. The prompt box counts as the tile's own input (`lib/focus.ts`, class `acp-prompt__input`), so switching to another tile moves focus off it as it would off a terminal.
- **Launcher Tab.** Tab now cycles the launcher's own fields: filter box, prompt box, worktree toggle, branch box and the Terminal/ACP radio group (one stop, at its checked radio). Before this it moved the agent selection whenever no prompt box was shown; the arrows still do that. The help overlay's "Launcher & dialogs" group says so.
- **Client bounds.** The client copy keeps at most `MAX_ITEMS` (2000, the daemon's cap) items, drops the oldest first, refetches rather than place a chunk for an item it does not hold, and keeps unchanged items by identity so memoized rows skip a re-render.
