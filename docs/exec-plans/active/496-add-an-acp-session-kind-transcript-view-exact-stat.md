# Add an ACP session kind: transcript view, exact state, permission prompts, typed results, PTY takeover

- **Spec:** [docs/product-specs/496-add-an-acp-session-kind-transcript-view-exact-stat.md](../../product-specs/496-add-an-acp-session-kind-transcript-view-exact-stat.md)
- **Issue:** #496
- **Status:** active
- **Phase:** 1 of 4
- **PR:** #499
- **Branch:** feature/496-acp-session-kind

## Summary

Add an `acp` session kind that `hived` owns next to `pty`: the daemon speaks ACP (JSON-RPC over stdio) to an adapter, the GUI renders a transcript instead of a terminal, and a session can be taken over in a PTY and handed back. The full *why* lives in the spec and in `docs/design-docs/acp-workflows.md` (spec 492's findings).

## Research

Detailed findings, with line citations, are in [docs/design-docs/acp-session-kind.md](../../design-docs/acp-session-kind.md). The parts that drive the plan:

### Relevant code

- **Spawn path.** `Registry.Create` (`internal/registry/create.go:96`) calls `insertEntry` (:600), which persists the entry before the spawn (:177).
  - A `Kind` field has to be threaded through `Entry`, `MetaFile`, `load()`, `Tombstone`/`Restore`, `Info()`, `wire.SessionInfo` and `wire.CreateSpec`.
  - An empty `Kind` reads as `pty`.
- **Restart paths.** `reviveAll` → `Revive` (registry.go:1315), `Restart` (:1422) and `Restore` all assume a PTY. ACP entries need a `session/load` branch in each.
- **Session coupling.** `internal/session.Session` is PTY-only and concrete. Plan: keep a separate `e.acp` child in `Entry`, and widen `Alive`, `Kill`, `Close`, `Restart`, `watchSessionExit` and `title` to cover it. `serveAttach` refuses ACP sessions.
- **State tier.** `internal/agentstate/machine.go`: feed `Apply` in-process with `Source: "acp"`. The tier must be exempt from the 30s `HookStaleAfter` demotion. Add the constant in `wire/control.go:327`.
- **Wire.** New frames start at 0x3f. Each must be classified, mapped in `controlEvents`, given a row in `docs/plugins.md`, and added to all three clients (GUI, ws-bridge, testclient). `DaemonContract` goes 21 → 22.
- **Child process.** Use `proc.CommandContext` and the plugin manager's supervise and kill-tree pattern (`internal/plugin/manager.go:640-695`).
- **GUI.**
  - The tile seam is `ensureTerm` (`session-term.ts:1872`). The new `AcpTile` implements `TermTile` (`app/state.ts:254`).
  - Focus code hard-codes the xterm textarea (`focus.ts:229`).
  - The launcher toggle copies the worktree toggle (`Launcher.tsx:567`).
  - The stream store copies `store/activity.ts`.
  - The bridge parity test requires mock and real-bridge stubs.

### Constraints and dependencies

- **No per-agent permission or unattended setting exists** (`internal/agent/settings.go:40`). Criterion 5 needs a new setting, a Settings UI for it, and a default.
- **No Node detection exists**, and the daemon's PATH usually lacks `npx`. The login-shell PATH has to be resolved without an interactive shell on the JSON-RPC pipes.
- **`SpawnedBy` exists nowhere in code.** Plan 391 defines it but hasn't started; it waits on 389 and 390. 496 lands it first, with the semantics 391 specifies: the daemon stamps it and the client never sends it.
- **Proven and unproven ACP methods.** Spike 492 proved `initialize`, `session/new`, `session/prompt`, `session/load` and `request_permission`. `cancel` and `set_mode` are unproven, and so is the PTY-to-ACP hand-back.
- **F2.** No known way to release the Codex writer lock.
- **F5.** Pi needs `submit_result` through the Hive Pi extension.
- **Gate B** needs ≥5 *real* tasks per agent (Claude, Pi), each taken over and handed back once. That spends the operator's subscription and needs real CLIs, so CI can't do it.
- **Size.** This is a large change across the daemon, wire, GUI, settings and docs. Phasing is an open question (see the Decision log).

### Prior lessons

- No hive-brain entries matched the ACP or daemon terms.
- Relevant GUI entries:
  - `hive-attach-disconnect-can-precede-opensession`: a Wails event can arrive before the binding's promise resolves, so the transcript subscribe path needs an epoch guard.
  - `hive-macos-menu-owns-cmd-chords`: a ⌘ chord change needs a `menu_darwin.go` change.
- From the 492 decision log:
  - Strip `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION` and `HIVE_SOCKET` from adapter env, and keep user preferences.
  - Match an agent's verbatim reply, not a derived string.

### Conventions card

- Build: `./build.sh`. Tests: `scripts/test.sh [go|unit|dom|e2e]`. Real-daemon e2e: `npm run test:e2e:real`, with `HIVE_SOCKET` and `HIVE_STATE_DIR` isolated.
- Lint gates:
  - `for os in darwin linux windows; do GOOS=$os staticcheck ./...; GOOS=$os go vet ./...; done`
  - `scripts/ui-lint.sh --strict`
  - `biome ci .` (in the frontend)
  - `npm run typecheck`, which needs `./scripts/ci-bootstrap.sh` first
  - `scripts/check-daemon-contract.sh main HEAD`
  - Check under the CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- TDD, so every behaviour change ships with its test. Go tests live beside the source; frontend tests live under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- **Wire changes:** JSON tags are `snake_case`, JS reads `snake_case ?? camelCase`, the three clients change in lock-step, and `DaemonContract` is bumped. Do not bump `PROTOCOL_VERSION`.
- **Children:** spawn only through `proc.Command*`. The GUI never spawns processes; everything goes over the wire.
- **New commands:** a title, a `shortcuts.ts` row, a menu item plus the accelerators JSON, the README Keybinds table and a changeset. A new key scope needs an `every-shortcut` fixture.
- **UI:** tokens and sprite icons only; a mock in `docs/design-docs/ui/mocks/` for any new visual choice; a `components.md` section for a new component.
- **Docs:** add a changeset under `.changesets/`, an entry in `site/features.json` with `since: "Unreleased"`, a `DESIGN.md` update (new package), and updates to `control-plane.md` and `acp-workflows.md`.

## Approach

### How it's built

- **ACP client.** A small JSON-RPC client written for Hive, in a new package `internal/acp`.
  - It talks newline-delimited JSON-RPC 2.0 over the adapter's stdio.
  - It speaks only the five methods spike 492 proved, plus `session/request_permission`. P3 adds `session/set_mode` after a probe.
  - It advertises no `fs` and no `terminal` capability.
  - Why not a Go SDK: there is no official one, and `coder/acp-go-sdk` lags the schema.
- **Registry.** The registry holds the ACP child in `e.acp`, next to `e.sess`. There is no interface over `session.Session`: ACP has no screen, resize or replay, so an interface would be mostly stubs.
- **One revive path.** Boot revive, `Restart` and `Restore` already all go through `Revive` (`registry.go:1287`, `:1508`, `closed.go:398`). So one `Kind == acp` branch there covers restart (P1). Takeover and hand-back are then just "persist the new kind, then `Restart`" (P4).
- **State.** ACP updates go into the agentstate machine in-process, with `Source: "acp"`.
  - The tier is exempt from staleness: `tickStates` already skips entries with no PTY, and `StaleAt` gets an `acp` exemption.
  - Hook and extension events are dropped for ACP sessions, so Pi's extension can't flip the tier.
- **Transcript after a restart** comes from `session/load` replay; the daemon doesn't persist its own copy. Turns typed in a PTY during takeover exist only in the agent's own store, so a daemon copy would drift from it. The in-memory transcript is capped.
- **GUI.** An ACP tile is still a `SessionTerm`: its attach is gated on kind, and the transcript is a React component portalled into the tile's overlays. This follows `ActivityTileMount` (`TileChrome.tsx:103-114`). A kind flip during takeover then starts or stops the attach without replacing the tile.
- **Login PATH.** `resolveLoginPATH` moves from `cmd/hivegui/shell_env_darwin.go` to `proc.LoginPATH()`. It runs once, as a marker-parsed `-i -l` probe, so nvm and fnm installs of Node are found, and it never touches the RPC pipes. Adapters exec `npx` directly with that PATH.
- **Typed result** (P3).
  - **Server.** `hived mcp-submit` is a stdio MCP server, one per spawn.
  - **Nonce.** It lives in the server name and env only, never in the tool's arguments, so the model can't see it and another session can't forge a result.
  - **Delivery.** The server sends `SUBMIT_RESULT` over the session's existing ModeSession connection, bound to that session's own id.
  - **Permissions.** Only the exact `mcp__hive-<nonce>__submit_result` identity is auto-allowed, and a request with no identity fails closed.
  - **Pi** registers `submit_result` through the Hive Pi extension.
- **Trust** (P3). A new per-agent setting, the ACP mode ceiling, defaults to the most restrictive value for each agent.
  - After every `session/new` and `session/load`, the daemon calls `session/set_mode` with the ceiling. If that fails, or the adapter reports a different mode, the child is killed.
  - Pi is refused unless the user's setting is "unattended".
- **Provenance** (P1). The daemon stamps `SpawnedBy` from the connection, in the principal format acp-workflows.md:226-231 defines: `session:<id>` and `workflow:<run-id>`. A row the user creates gets `""`.
  - P1 adds `plugin:<id>` for a plugin socket, and documents it in acp-workflows.md.
  - `SpawnedBy` is `json:"-"` on `CreateSpec`, so a client can't send it.
  - P1 also edits plan 391 so its spawner check compares against `session:<ownSessionID>`, not the bare id.
  - Every prompt records an `origin` principal in the same format, both on the transcript item and in the activity event.
- **One prompt at a time.** ACP allows one prompt in flight per session. `PromptACP` rejects a second prompt while a turn is running, with `ErrCodeACPBusy`, and does not queue it. The caller decides whether to retry, so a result and its origin always attach to the right turn.
- **Codex hand-back (F2).** Hive detects the adapter's `already has an active writer` error and returns a clear error, then reverts the session to PTY. It never kills a process it didn't spawn.

### Phases

| Phase | PR content | DaemonContract |
|---|---|---|
| P1 daemon core | `internal/acp`, Kind, registry lifecycle, `acp` tier, transcript, prompt, permission frames, `SpawnedBy`, `ACPSpec` + availability, `proc.LoginPATH` | 21 → 22 |
| P2 GUI | transcript tile, store, launcher toggle, bindings, mocks, key scopes, UI mock | none (GUI and ws-bridge only) |
| P3 typed result + trust | `hived mcp-submit`, `SUBMIT_RESULT`, permission matcher, Pi tool, mode ceiling setting + UI, Pi gate | 22 → 23 |
| P4 takeover + docs | `SET_SESSION_KIND`, `SetKind`, F2 handling, commands and menu, `control-plane.md`, `acp-workflows.md` gate B, DESIGN/AGENTS/README, changeset, `features.json` | 23 → 24 |

The contract bumps once per phase because `scripts/check-daemon-contract.sh:36-40` fails every daemon-touching PR that doesn't bump it.

## Files to change

### P1

1. `internal/agent/agent.go:29`: `Def.ACP *ACPSpec{Argv, NeedsNode, Experimental, Modes}`.
   - Pinned adapters:
     - Claude: `npx -y @agentclientprotocol/claude-agent-acp@0.85.1`
     - Codex: `npx -y @agentclientprotocol/codex-acp@2.1.1`
     - Pi: `npx -y pi-acp@0.0.34`
   - Experimental, native: Gemini `gemini --acp`, Copilot `copilot --acp --stdio`.
   - Add `ACPAvailable() (bool, reason string)`.
2. `internal/proc/loginpath_{unix,windows}.go` (new): moved from `cmd/hivegui/shell_env_darwin.go:43-140`. Move `OwnGroup` and `KillTree` from `internal/plugin/kill_{unix,windows}.go` into `internal/proc`, and update `internal/plugin/manager.go:671` to call them.
3. `cmd/hivegui/shell_env_darwin.go`: call `proc.LoginPATH()`.
4. `internal/wire/control.go`:
   - `CreateSpec.Kind`, and `CreateSpec.SpawnedBy` with tag `json:"-"`.
   - `SessionInfo.Kind` and `SessionInfo.SpawnedBy`.
   - `StateSourceACP`, `KindPTY`, `KindACP`.
   - New types: `GetAcpTranscriptReq`, `AcpTranscriptMsg{session_id, epoch, reset, items, pending_permission}`, `PromptAcpReq`, `AnswerPermissionReq`.
5. `internal/wire/frame.go`:
   - New frames: `0x3f GET_ACP_TRANSCRIPT`, `0x40 ACP_TRANSCRIPT` (both reply and push), `0x41 PROMPT_ACP`, `0x42 ANSWER_PERMISSION`.
   - Classify them and add `String()`.
   - `internal/wire/client.go:120`: map `ACP_TRANSCRIPT` to `"acp:transcript"`, avoiding `transcript:*`, which spec 431 owns.
6. `internal/registry/registry.go`:
   - New `Entry` fields: `Kind`, `SpawnedBy`, `acp`, `acpTx`, `acpEpoch`.
   - `Alive()`, `Info()`.
   - `Revive` (guard widened, ACP branch after cwd resolution at :1357).
   - Teardown in `Restart`, `kill` (:1731) and `Close` (:1901).
   - `ApplyAgentEvent` drops hook and extension events for ACP.
   - Both `load()` literals, and `persistEntryLocked`.
7. `internal/registry/persist.go:12`: `MetaFile.Kind` and `MetaFile.SpawnedBy`, `omitempty`. An empty `Kind` means `pty`.
8. `internal/registry/closed.go:111`, `~:355`: the tombstone and restore literals carry the new fields.
9. `internal/registry/create.go`:
   - `insertEntry` sets `Kind` and `SpawnedBy`.
   - `finishCreateTail` (:177) branches to `startACP`.
   - `Kind == acp` is rejected when the agent has no ACP spec, or with `Cmd` or `ContinueConversation` set. An `InitialPrompt` is sent as the first `PromptACP`.
10. `internal/agentstate/machine.go`: `trusted` (:274), `StaleAt` (:672) and `Apply` accept `acp`, with no staleness.
11. `internal/daemon/daemon.go`:
    - Three new arms in `handleControlFrame` (:1446), going through `runOp`. None of them go in `sessionModeFrames`.
    - `SpawnedBy` stamping on create.
    - `origin` on `PROMPT_ACP`.
    - `serveAttach` (:1894) returns `ErrCodeACPSession` for an ACP session.
12. `internal/buildinfo/contract.go`: 22, with a history line.
13. `cmd/hived-ws-bridge/main.go` and `internal/wire/testclient/client.go`: the three new methods.
14. `docs/plugins.md`: frame rows, checked by the parity test.
16. `docs/design-docs/acp-workflows.md:226-231`: add the `plugin:<id>` principal. `docs/exec-plans/active/391-agent-spawned-sessions.md:42-48`: compare against `session:<ownSessionID>`.
17. `internal/plugin/install.go:140-141`: call `proc.OwnGroup` and `proc.KillTree`. The plugin functions being moved are named `ownGroup` and `killTree` today; the move exports them.
18. Test seam: `acpLaunchForTest` in `internal/registry` swaps the pinned argv for the re-executed fake. Each test registers a temporary catalog def through the existing custom-agent path and removes it with `t.Cleanup`.
15. `cmd/hivegui/frontend/src/lib/activity.ts:160`: `isStale` exempts `acp`. This one-line frontend change lands with P1, so stale labels stay right from the first PR.

### P2

1. `src/app/state.ts`: `SessionInfo.kind` and `SessionInfo.spawned_by`, read as `snake_case ?? camelCase`.
2. `src/app/session-term.ts`: `ensureAttached` returns `'deferred'` and hides xterm for `acp`; `setInfo` re-runs attach when the kind flips.
3. `src/components/TileChrome.tsx:86`: `<AcpTranscriptMount>`.
4. `src/app/focus.ts:229-241`: an ACP tile focuses `.acp-prompt textarea`.
5. `src/app/events.ts:742`: `EventsOn('acp:transcript')`.
6. `src/components/modals/Launcher.tsx:567-604`: Terminal/ACP toggle.
   - Defaults to Terminal.
   - Disabled with `acpReason` when ACP isn't available.
   - Shows an Experimental chip for Gemini and Copilot.
   - `launchSelected` passes `kind`.
7. `cmd/hivegui/app_calls.go`:
   - `AgentInfo` gains `acp`, `acpAvailable`, `acpReason`, `acpExperimental`.
   - `CreateSession` passes `Kind`.
   - New bindings: `GetAcpTranscript`, `PromptAcp`, `AnswerPermission`.
8. `src/bridge.ts`, `test/e2e/wails-mock.ts`, `test/e2e-real/wails-bridge.ts`: kept in parity.
9. `src/app/key-scopes.ts`:
   - A `text-input` scope for the prompt: Enter sends, Shift+Enter adds a newline.
   - A `matched` scope for Allow and Deny.
   - A `FIXTURES` entry in `every-shortcut.spec.ts`.
10. `docs/design-docs/ui/components.md` and the README decision table: the transcript component.

### P3

1. `cmd/hived/main.go:34`: dispatch `mcp-submit`.
2. `internal/wire`: `0x43 SUBMIT_RESULT`, a control request, also added to `sessionModeFrames` (daemon.go:1315). `AcpTranscriptMsg` gains `result`, `result_status` and `prompt_id`.
3. `internal/registry/acp.go`:
   - Nonce, and the MCP server list sent on both new and load.
   - Permission decision.
   - `set_mode` to the ceiling after new and load; kill on mismatch.
   - Pi gate.
   - `SubmitResult`, size-capped and nonce-checked.
   - Every prompt clears the result; a turn that ends with none sets `result_status: none` (F3).
4. `internal/agent/settings.go:40`: `ACPModeCeiling map[string]string`, defaulting to the most restrictive. Mirror it in `cmd/hivegui/app_calls.go:119-155`, and add a per-agent select in the Agents tab of `Settings.tsx`.
5. `internal/agent/pi/hive.ts`: the `submit_result` tool, registered only when `HIVE_SUBMIT_NONCE` is set.
6. `contract.go`: 23. Update the three clients and `docs/plugins.md`.

### P4

1. `internal/registry/acp.go`: `SetKind(id, kind)`.
   - **Precheck for every agent:** `AgentSessionID` must be set.
   - **Agents with `TranscriptPaths`** (Claude, Pi): the transcript must also exist, so a missing one fails closed instead of `ResumeArgs` falling back to `--session-id`.
   - **Agents without it** (Codex): `ResumeArgs` is `codex resume <id>`, which has no fresh-session fallback, so the id check is enough.
   - **Session ids:**
     - **Claude and Codex.** The ACP id is used as-is as the CLI resume id. The probe only assumed this: its `cliSessionId` passes the id through unchanged (`agents.mjs:92`, `:116`). The evidence is F6, which reopened both in a PTY by that id.
     - **Pi.** The ACP id is resolved through `~/.pi/pi-acp/session-map.json`, as `agents.mjs:37-41` does. If the id is missing from the map, takeover fails closed.
     - **Tests.** `TestTakeoverPiResolvesSessionMap` and `TestTakeoverPiUnmappedRefused`.
   - Persist the new kind, then call `Restart`.
   - On a failed hand-back, revert to `pty` and `Restart` once.
   - Map the F2 writer-lock error.
2. `internal/daemon/daemon.go`: a `SET_SESSION_KIND` (0x44) arm through `runOp`. Add the frame in `internal/wire`, bump the contract to 24, and update the three clients, the bridges and the mocks.
3. GUI commands `take-over-session` and `hand-back-session`.
   - Each declines when the session isn't the right kind.
   - Rows in `shortcuts.ts` with no default key.
   - `menu_darwin.go` items, plus `testdata/menu-default-accelerators.json`.
   - The README Keybinds table.
4. Docs:
   - `docs/design-docs/control-plane.md:44` and `:298`: the `acp` tier is implemented.
   - `docs/design-docs/index.md:11`: drop "`acp` proposed", and add an entry for `acp-session-kind.md`.
   - `docs/design-docs/acp-workflows.md`: gate B becomes Claude + Pi, with a run log table (date, agent, task, outcome). Also update the spec-1 text (:279-282) and the node table row (:179). Leave the verdict rule alone.
   - `DESIGN.md` and the AGENTS.md package table: add `internal/acp`.
   - `docs/design-docs/acp-session-kind.md`: final notes.
5. `.changesets/496-acp-session-kind.md` (`added`, `minor`) and a `site/features.json` entry (`since: "Unreleased"`).

## New files

| Path | Phase | Purpose |
|---|---|---|
| `internal/acp/conn.go` | P1 | JSON-RPC conn: `Call`, `Notify`, `Handle`; -32601 for unknown methods; malformed lines are skipped |
| `internal/acp/types.go` | P1 | ACP v1 request and update types (a tagged union) |
| `internal/acp/agent.go` | P1 | Starts the adapter through `proc.CommandContext` in its own process group, with `WaitDelay` and a stderr ring for `LastError`. Env: strips `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `HIVE_*`; keeps user preferences; `PATH=LoginPATH`. Methods: `New`, `Load`, `Prompt`, `Close` (which runs `KillTree`) |
| `internal/acp/transcript.go` | P1 | Coalesces chunks, merges tool updates by id, epoch, capped ring, origin on user items |
| `internal/acp/acptest/fake.go` | P1 | Scripted fake agent, a Go port of `fake-agent.mjs`. History in `$HIVE_FAKE_ACP_DIR/<sid>.jsonl`; `session/load` replays it. It can request permission and can report the writer lock |
| `internal/registry/acp.go` | P1 | `startACP`, `reviveACP`, `watchACPExit`, `PromptACP`, `AnswerPermission`, and the event mapping (no machine events while a load replays) |
| `src/store/acp.ts` | P2 | Snapshot plus deltas, with an epoch guard |
| `src/components/acp/AcpTranscript.tsx` | P2 | Markdown agent text, plan and tool rows reused from `components/activity`, the permission card, the prompt textarea |
| `src/theme/components/acp.css` | P2 | Tokens only |
| `docs/design-docs/ui/mocks/acp-transcript.html` | P2 | Required mock |
| `cmd/hived/mcpsubmit.go` | P3 | stdio MCP server, a port of `submit-mcp.mjs` |
| `internal/acp/permission.go` | P3 | `toolIdentity` and `decidePermission`, ported from `probe.mjs:152-184` |

## Tests

### P1

- `internal/acp/conn_test.go`: `TestCallMatchesReplyByID`, `TestHandleUnknownMethodReturnsMethodNotFound`, `TestNonJSONLineIsSkipped`.
- `internal/acp/transcript_test.go`: `TestChunksCoalesce`, `TestToolCallUpdateMergesByID`, `TestResetBumpsEpoch`, `TestCapDropsOldest`.
- `internal/acp/agent_test.go`: `TestStartNewPromptRoundTrip`, `TestEnvStripsClaudeAndHiveVars`, `TestCloseKillsTree`. These run the fake agent by re-executing the test binary from `TestMain`.
- `internal/registry/acp_test.go`:
  - `TestCreateACPPersistsKind`
  - `TestEmptyKindLoadsAsPTY`
  - `TestACPStateWorkingPermissionIdle`: `state_source == "acp"`
  - `TestACPNotStaleAfterHookStaleAfter`
  - `TestReviveACPUsesSessionLoadAndReplaysTranscript`
  - `TestRestartACPReloads`
  - `TestRestoreACP`
  - `TestHookEventIgnoredForACP`
  - `TestPromptRecordsOrigin`
  - `TestPromptWhileWorkingRejected`: returns `ErrCodeACPBusy`, and the fake receives exactly one `session/prompt`.
  - `TestCreateACPRejectsAgentWithoutSpec`
- `internal/daemon/acp_test.go`:
  - `TestAttachRefusesACPSession`
  - `TestSpawnedByStampedFromPluginSocketNotClient`: asserts the exact `plugin:<id>` string, and that a `spawned_by` sent in client JSON is ignored.
  - `TestGetAcpTranscriptUnknownSession`
- `internal/agent/agent_test.go`: `TestACPSpecsPinned`, `TestACPAvailableNoNodeReason`.
- `internal/proc/loginpath_unix_test.go`: the existing `resolveLoginPATH` tests, moved here.
- `test/unit/activity.test.ts`: `isStale false for acp`.

### P2

- `test/unit/acp-store.test.ts`: stale epoch dropped; reset replaces; a delta that arrives before the snapshot is kept.
- `test/dom/acp-transcript.test.tsx`: renders markdown, plan and tool rows; Allow sends the allow option id; Enter sends, Shift+Enter adds a newline.
- `test/dom/launcher.test.tsx`: defaults to Terminal; ACP disabled with a reason; experimental chip.
- `test/dom/tile-chrome.test.tsx`: an ACP tile mounts the transcript and never calls `OpenSession`.
- `test/e2e/acp-session.spec.ts`: launch an ACP session from the mock, answer a permission inline, send a prompt.
- Existing coverage, run as-is: `bridge-harness-parity` and `every-shortcut`.

### P3

- `internal/acp/permission_test.go`: `TestAllowsOnlyExactIdentity`, `TestIdentityFromEarlierToolCall`, `TestUserServerNamedHiveRejected`, `TestMissingIdentityFailsClosed`.
- `cmd/hived/mcpsubmit_test.go`: `TestToolsListAdvertisesSubmitResult`, `TestToolsCallSendsSubmitFrame`, `TestNonceNotInToolSchema`.
- `internal/registry/acp_result_test.go`: `TestSubmitResultWrongNonceRejected`, `TestResultClearedOnNextPrompt`, `TestTurnEndWithoutResultReportsNone`, `TestSubmitAutoAllowedOtherPermissionWaits`, `TestMCPServersResentOnLoad`.
- `internal/registry/acp_trust_test.go`: `TestModeSetToCeilingAfterNew`, `TestSetModeFailureKillsSession`, `TestPiRefusedWithoutUnattended`, `TestCeilingDefaultsMostRestrictive`.
- The Pi extension test: `submit_result` is registered only when `HIVE_SUBMIT_NONCE` is set.
- `test/dom/settings.test.tsx`: the ceiling select saves per agent.

### P4

- `internal/registry/acp_takeover_test.go`:
  - `TestTakeoverHandBackLosesNoTurn`. The fake agent writes turns 1-2. The PTY phase runs the test binary as a fake CLI that appends turn 3 to the same history. After hand-back the test asserts **Hive's** state, not the fake's replay:
    - the registry transcript's user and agent items are exactly turns 1-3, in order, with no duplicates;
    - the epoch was bumped;
    - the fake recorded `session/load` with the original session id, and no `session/new`;
    - `Kind == acp` is persisted in `session.json`;
    - a 4th prompt then round-trips.
  - `TestTakeoverRefusedWithoutTranscript` (Claude-like def), and `TestTakeoverCodexLikeNeedsOnlySessionID` (def with no `TranscriptPaths`).
  - `TestHandBackWriterLockedClearErrorAndRevertsToPTY`
  - `TestKindFlipPersisted`
- `test/e2e/acp-session.spec.ts`: take over, then hand back, from the palette.
- `node scripts/acp-probe/check-doc.mjs docs/design-docs/acp-workflows.md` still passes.

## Verification

### Every phase

- `scripts/test.sh` (go, unit, dom, e2e)
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done`, under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`
- `scripts/check-daemon-contract.sh origin/main HEAD`. It fails if a daemon phase forgets to bump the contract.

### Per phase

- **P1:**
  - `go test ./internal/acp/... ./internal/registry/ ./internal/daemon/ -run 'ACP|Kind|SpawnedBy|Attach' -count=1 -race`
  - `go test ./internal/proc -run TestNoDirectExecOnWindows`, which fails if `internal/acp` uses `os/exec` directly
- **P2:**
  - `./scripts/ci-bootstrap.sh && npm run typecheck`
  - `biome ci .`
  - `scripts/ui-lint.sh --strict`
  - `CI=1 npx playwright test acp-session every-shortcut`
- **P3:** `go test ./internal/acp/ ./cmd/hived/ ./internal/registry/ -run 'Permission|Submit|Ceiling|Pi' -count=1`, plus the Pi extension test.
- **P4:**
  - `npm run test:e2e:real`, isolated with `HIVE_SOCKET` and `HIVE_STATE_DIR`
  - `node scripts/acp-probe/check-doc.mjs docs/design-docs/acp-workflows.md` (the same invocation as `ci.yml:359`)
  - the operator's gate B run log (≥5 runs per agent for Claude and Pi), filled in before P4's gate

## Criteria → phase

| # | Criterion | Phase |
|---|---|---|
| 1 | `acp` kind created via `Registry.Create`, persisted, on the wire; Terminal is the default | P1 (dialog default: P2) |
| 2 | Transcript view; no terminal attached | P2 (daemon refusal: P1) |
| 3 | `acp` state tier | P1 (control-plane.md: P4) |
| 4 | Typed result, prompt op with provenance, only `submit_result` auto-allowed | P1 prompt and origin; P3 result and matcher |
| 5 | Trust: mode ceiling (F4), Pi gate (F5), `SpawnedBy` | P1 `SpawnedBy`; P3 ceiling and Pi gate |
| 6 | Takeover and hand-back, F2, no-lost-turn test | P4 |
| 7 | Restart via `session/load` | P1 |
| 8 | Pinned adapters, no-Node reason, experimental label | P1 spec and reason; P2 UI |
| 9 | Gate B update and run log | P4 (the runs themselves are the operator's) |

## Risks

- **Q1. Staleness exemption.** It's safe:
  - `tickStates` skips sessions with no PTY (`registry.go:1077`), and the Laya classifier skips them too (`classify.go:149`). Liveness comes from process exit.
  - A hung adapter shows `working`, which is accurate, because its prompt RPC is still outstanding.
  - The tier can only be flipped if hook and extension events get through, and P1 drops them.
- **Q2. Pi session ids.**
  - The probe measured the ACP id equal to the Pi session id (`results/pi-*.json`: `session_id_mismatch: false`).
  - The takeover precheck fails closed if that ever diverges.
- **Q3. Transcript after a restart.**
  - Replay only; the daemon does not persist a copy.
  - Risk: adapters may replay only message text, not tool or plan rows. The gate B run log gets a "replayed after restart" column, filled in per agent, so whether criterion 7 holds is recorded rather than assumed. Add a cache only if users miss those rows.
- **Q4. Prompt provenance.** `origin` is stored on the in-memory transcript item and in the activity event.
  - Prompts replayed after a restart are labelled "replayed".
  - A durable audit log is #495's job.
- **Q5. Pi extension under pi-acp is unknown.** It is unverified whether pi-acp passes `-e`/env through to `pi --mode rpc`, and that blocks criterion 4 for Pi.
  - P3's first task is to read the pi-acp 0.0.34 source.
  - If it doesn't pass them through, the fallback is an upstream PR or pi's auto-discovered extension directory. Both need the operator's OK.
- **Q6. `session/set_mode` is unproven.** P3's first task is a probe run for Claude and Codex. If an adapter lacks it, Hive refuses ACP for that agent rather than running it in its default mode.
- **Q7. PTY-to-ACP hand-back was never probed against real adapters.** The fake-agent test proves Hive's mechanics; gate B is the real evidence.
- **Q8. The ceiling lives in agent settings**, the same exposure as every other setting. Noted, not solved here.
- **Q10. Login PATH on Linux.** Moving the probe from a darwin-only file into `loginpath_unix` turns on a one-shot `-i -l` shell probe (bounded at 10s) on Linux too.
  - `hived` runs it lazily, the first time ACP availability is checked or an ACP session is spawned, through `sync.OnceValue`, and never under `r.mu`.
  - `cmd/hivegui/shell_env_windows.go` stays as it is.
- **Q11. Availability is computed in the GUI process**, from the `LoginPATH` the GUI sees, but the daemon is the one that spawns. If the two disagree, the spawn fails with `LastError` shown on the tile. If that happens in practice, move availability onto the wire.
- **Q12. Phases under the pipeline.** `/hs-merge-gate` supports `Phase: N of M`. For P1-P3 it records a per-phase PASS and keeps the spec at `GATE`; the operator resets it to `IMPLEMENT` for the next phase.
- **Q9. Env.** Adapter children get every inherited `HIVE_*` variable stripped. The session's own `HIVE_*` values are added back only through the MCP server's env and Pi's submit env.
- **Ruled out:**
  - `session/cancel`: unproven, and no criterion needs it.
  - An `AcpTile` interface implementation: the tile contract has about 25 members, and a kind flip would mean swapping tiles.
  - Hive persisting transcripts: see Q3.
  - A Go ACP SDK: see the ACP client bullet.

## Second opinion

- **Round 1** — verdict: revise, confidence 7. Six must-fix items, all applied:
  1. Codex takeover precheck when an agent has no `TranscriptPaths`, plus the session-id mapping.
  2. `SpawnedBy` format aligned with acp-workflows.md and plan 391.
  3. The no-lost-turn test asserts Hive's transcript, epoch, `session/load` and persisted `Kind`, not the fake's replay.
  4. The `check-doc.mjs` doc-path argument.
  5. The concurrent-prompt rule: reject with `ErrCodeACPBusy`, with a test.
  6. Missing docs: `design-docs/index.md` and `control-plane.md:298`.

  Nice-to-haves also applied: the `install.go` call site, Linux login-PATH laziness (Q10), GUI-vs-daemon availability (Q11), phases under merge-gate (Q12), `origin/main` for the contract check, and a replay column in the gate B log.
- **Round 2** — verdict: approve, confidence 8. No must-fix items. Its three nits were applied:
  - Pi resolves ids through pi-acp's session map, with tests;
  - the Claude and Codex id mapping is labelled as assumed, with F6 as the evidence;
  - the `check-doc` argument is added in the test list too.

## Decision log

- **2026-10-03** — Review loop extended past 5 iterations (operator approved up to 3 more); it converged at iter 6. Iters 1–5 each surfaced one real, shrinking issue in new code (worktree binding, PATH resolution, stderr pipe, tool-content arrays, escaped-size budget, replay flood), all fixed with mutation-checked tests. Five MINORs from iter 6 are deferred to phase 2, which reworks the transcript path anyway: O(n²) chunk concat and trim copies, per-chunk copies during replay, untested slow-listener drop. (The startACP failure-branch tests were added after all, in ce2293fe, after CodeRabbit flagged the deferral against AGENTS.md.)
- **2026-10-03** — P1 updates `control-plane.md` (acp tier implemented), DESIGN.md and AGENTS.md now, not in P4. Why: the tier and the `internal/acp` package exist from P1, and docs that lag the code are a bug.
- **2026-10-03** — P1 ships a changeset after all. Why: plugins can create and drive ACP sessions from P1, which is user-visible to plugin authors; the pre-push gate is right.
- **2026-10-03** — `isStale` in the GUI was left unchanged. Why: the daemon sends no `stale_at` for the acp tier, so the existing check already returns false.
- **2026-10-03** — The plugin SDK typedefs and frame table gained the ACP frames and fields, and the webhook plugin's vendored SDK was re-copied. Why: drift tests in `internal/plugin` require it. The plan missed this blast radius.
- **2026-10-03** — Ship in 4 phases (daemon core, GUI, typed result + trust, takeover + docs), one PR each. Why: the change is large; per-phase gates keep review tractable, and `/hs-merge-gate` supports `Phase: N of M`.
- **2026-10-03** — Hand-rolled JSON-RPC client, no Go ACP SDK. Why: none is official; `coder/acp-go-sdk` lags the schema.
- **2026-10-03** — Transcript after restart comes from `session/load` replay, not daemon persistence. Why: PTY-typed turns during takeover live only in the agent's store; a daemon copy would diverge.
- **2026-10-03** — Gate B targets Claude + Pi, not Claude + Codex. Why: the operator named Claude and Pi as the primary agents during the 496 brainstorm.
- **2026-10-03** — ACP sessions must survive a `hived` restart in the same conversation. Why: PTY sessions already do (`reviveAll` → `Revive` → `ResumeArgs`), so ACP must match.

## Progress

- **2026-10-03** — Plan created; research started.
- **2026-10-03** — Research done; plan approved (second opinion: revise→approve, 8/10). Phase 1 of 4 starts.
- **2026-10-03** — Phase 1 implemented and PR #499 opened (daemon core, contract 22).

## Open questions

## PR convergence ledger

- **2026-10-03 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: acbc2b4cece9d2b0bbf20a49c764298ffcddb6f6a59a26127a1aee30cda32b2b; threads_open: 4; action: escalated:ci-check-failed-and-risky-fixes; head_sha: 3849659b.
- **2026-10-03 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 407898cce71bcd371afd9741738315710c3cbd15b4ed5f0648955372f51c8c14; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: a01ee8b6.
- **2026-10-03 iter 3** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 4828cc3574dfd7aa554ed8663d7ad52a69f62f1bcd048362a6bb1d3bebe5e863; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 5b5e2a6d.
- **2026-10-03 iter 4** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 3c0a5fadf565d9f018a9e4ffa8ab5c48c8cc0ec4d058d01d5dd3b9819908c9ab; threads_open: 0; action: autofix+push; head_sha: 9061d336.
- **2026-10-03 iter 5** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 1eb37cc22a111b8b2bb97f337009b172b6f65c694c570362e568d21ee5091dc2; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 221e246f.
- **2026-10-03 iter 6** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 5cc750ed.
- **2026-10-03 iter 7** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: coderabbit-2-threads; threads_open: 0; action: autofix+push; head_sha: ce2293fe.
- **2026-10-03 iter 8** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 93368a92.

## Gate verdict

- **2026-10-03** — verdict: PASS; phase: 1/4; checks: 13 passed / 0 failed / 0 followups / 3 deferred; followups: none; one-line: phase 1 (daemon core) delivers every P1 part of criteria 1–5, 7, 8; criterion 6 and 9 (takeover, gate B) and the GUI/trust parts are deferred to phases 2–4.
  - 2026-10-03 dimensions:
    - acceptance — PASS — criteria 1, 3, 7 fully; 2, 4, 5, 8 P1 parts (attach refusal, PROMPT_ACP with origin, daemon-stamped SpawnedBy, pinned adapters + no-Node reason + experimental flag); 6 and 9 DEFERRED (phase 4); targeted tests pass in acp, registry, daemon, agent, agentstate, wire.
    - non-goals — PASS — PTY stays default and unchanged (empty kind persists as pty; only isACP-gated branches); text-only prompts; no picker, search, engine, bundled adapters or agent-side server.
    - doc accuracy — PASS — changeset valid; plugins.md, SDK and vendored copy match wire; control-plane, acp-workflows, DESIGN/AGENTS, index, contract 22 and plan 391 accurate. Two optional notes (CREATE_SESSION kind in plugins.md; review-round hardening in acp-session-kind.md) added in the same commit.
