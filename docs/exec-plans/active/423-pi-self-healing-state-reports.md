# Pi reports self-healing session state (no stale-turn guessing)

- **Spec:** [docs/product-specs/423-pi-self-healing-state-reports.md](../../product-specs/423-pi-self-healing-state-reports.md)
- **Issue:** #423
- **Status:** active
- **PR:** #424
- **Branch:** feature/423-raise-attention-when-agent-turn-goes-quiet

## Summary

The Pi extension reports one-shot edges, so a lost report leaves the daemon wrong until the tier goes stale and it guesses `idle`. Give every state-bearing extension event an ordering key `(instance, seq)`, and have the extension re-send its latest state-bearing event every 5 s. The daemon applies an unseen key and treats a seen one as liveness only. Lost reports heal within 5 s, attention the user cleared stays cleared, and a live Pi session never falls back to terminal guessing. This replaces the first approach on this PR (stale-turn tick plus focus dwell), which the operator dropped.

## Research

### Relevant code

- `internal/agent/pi/hive.ts`: `createSender` (l.158) serializes reports; `post`/`sender.send` build events; the handlers (l.306-431) map Pi events to kinds. There is no sequence number and no periodic send.
- `internal/agent/pi/hive.test.ts`: `node --test` suite with a fake unix-socket server (`collectConnections`) and `handlerPi()` for firing handlers.
- `internal/agent/pi_test.go` l.163: decodes `encodeFrames` output with the real Go wire reader, which is the cross-language contract check.
- `internal/wire/control.go` l.487: `AgentEvent`; optional fields round-trip unchanged. `jsonUnmarshal` in the daemon does not reject unknown fields.
- `internal/daemon/daemon.go` l.744 `serveEvent`/`applyEventFrame`: kind allowlist and source check, then `registry.ApplyAgentEvent`.
- `internal/registry/registry.go` `ApplyAgentEvent` (~l.458): clamps `at`, calls `Machine.Apply`, announces on change, and calls `broadcastActivityLocked` (events.go:115) **unconditionally**, so a 5 s heartbeat would broadcast activity for every Pi session unless replays skip it.
- `internal/agentstate/machine.go` `Apply` (~l.370): timestamp ordering guard (`orderAt`, bounded by `HookStaleAfter`); `hookSeenAt = ev.At` drives `trusted()` against the daemon clock. A replay keeps its original `at`, so it must refresh liveness from `ev.Now`, not `ev.At`, and must bypass the timestamp guard.
- `internal/agentstate/activity.go` l.150 `applyLateActivity`: merges late tool/plan events into the ring. A replay must never reach it (it would duplicate ring entries).
- `internal/buildinfo/contract.go`: `DaemonContract` must bump (CI `scripts/check-daemon-contract.sh`), history entry 13.
- `docs/design-docs/agent-activity.md` ~l.139: documents the Pi sender, to extend with ordering and heartbeat.

### Constraints / dependencies

- Old daemon, new extension: unknown JSON fields are ignored, so `instance`/`seq` are dropped. The 5 s heartbeat then re-applies its latest event through the timestamp guard, where an equal or newer stamp applies. That would re-raise a cleared wait every 5 s. So the extension sends heartbeats only when the daemon understands them; see the Approach section.
- A Pi spawned by the new daemon that outlives a daemon downgrade re-raises cleared waits every 5 s until it restarts.
- New daemon, old extension: no `instance`/`seq`, and the existing timestamp path is unchanged.
- `Snapshot` stays `==`-comparable, and `New(t0).Snapshot()` stays the zero value.

### Prior lessons

- No prior lessons matched (brain search `agentstate attention stale tick pi`).

### Conventions card

- Go: `go build ./... && go vet ./... && staticcheck ./... && go test ./...`
- Pi extension: `node --test internal/agent/pi/` (node ≥ 23.6; the Go test skips on older node).
- Frontend (untouched by this plan): `npm test`, `npx biome ci .`, `npm run typecheck` in `cmd/hivegui/frontend`.
- A daemon-observable wire change bumps `buildinfo.DaemonContract` with a history line.
- User-visible change: a `.changesets/*.md`. Comments explain *why*.

## Approach

### Ordering key (state-bearing events only)

State-bearing extension events carry `instance` (a random id minted each time the extension factory runs) and `seq` (1-based per instance). State-bearing kinds: `prompt`, `permission_resolved`, `tool_start`, `tool_end`, `turn_end`, `idle`, `error`, `waiting_input`, `waiting_permission`. `ping`, `plan` and `session_end` stay unkeyed and keep today's timestamp path, so a delivered plan or ping can never mask a lost state event.

Pi re-runs the extension factory for `/new`, `/resume`, fork and `/reload` (`AgentSessionRuntime.createRuntime`, after `teardownCurrent` emits `session_shutdown`), so each of those is a new instance. There is no clock in the key: a different instance is always accepted.

### Daemon

- `wire.AgentEvent`: `Instance string \`json:"instance,omitempty"\``, `Seq uint64 \`json:"seq,omitempty"\``. `applyEventFrame` refuses half a key, and a key on any source but `extension`, through the unknown-source refusal path. That closes the connection and drops later frames on it; a well-formed extension never sends either, so nothing real is lost.
- `agentstate.Event`: `Instance`, `Seq`. `Machine`: `extInstance`, `extSeq`, and `extState`: the session's state as of the extension tier's last say. Keyed `Apply` sets it to the resulting state, and `ClearWaiting` updates it while the source is `extension`, so a wait the user cleared is recorded as cleared.
- `Machine.Replay(ev) bool` (new): true for any keyed event with `ev.Instance == extInstance` and `ev.Seq <= extSeq`. When `source == extension` it refreshes liveness from `ev.Now` and changes nothing else. When the heuristic tier has taken the session (a stall longer than `HookStaleAfter`, then output), it **restores** instead: `state = extState`, `source = extension`, liveness from `ev.Now`. No text, no `endTurn`, no ring change, so a cleared wait stays cleared and a replayed `tool_start` adds no ring entry. It reports whether the snapshot changed, so the registry can announce a restore; a pure liveness replay announces nothing.
- `Machine.Apply`, keyed unseen event: skips the timestamp guard (the key already orders it), sets `extInstance`/`extSeq` (a different instance resets `extSeq` to `ev.Seq`), liveness from `ev.Now`, and `orderAt = max(orderAt, ev.At)`: never `ev.Now`, because a keyed `tool_end` and its unkeyed `plan` share one `at`, and the plan must not land behind the guard (the #421 plan-loss bug). Max, not assign, so a healed old `at` never moves the guard backwards. `StateExited` stays terminal.
- `registry.ApplyAgentEvent`: after `at` is clamped and `now` sampled, `if handled, changed := e.machine().Replay(ev); handled { if changed { announce }; return nil }` before `Apply`. A replay never broadcasts activity.
- `DaemonContract` 12 → 13 with a history line.

### Extension (`hive.ts`)

- Per factory run: `instance = crypto.randomUUID()`, `seq = 0`, `lastState = null`.
- `sender.send` stamps each state-bearing event with `instance` and `++seq`, and stores the encoded single-event report as `lastState`.
- `session_start` becomes state-bearing: it sends `idle` alongside its `ping`/`plan` when `ctx.isIdle()` is true, and `permission_resolved` (working) otherwise. `teardownCurrent` aborts the turn for `/new`/`/resume`/fork, but `/reload` (`AgentSession.reload`) does not, and an extension's `ctx.reload` can run mid-turn. This gives a new instance a `lastState` that heals a straggler from the old instance.
- Heartbeat: `setInterval(HEARTBEAT_MS = 5000).unref()`, started in the factory and cleared on **every** `session_shutdown` (quit or not; a non-quit shutdown is followed by a fresh factory run that starts its own). Each beat re-sends `lastState` byte-for-byte when `pending() === 0` and skips otherwise.
- Enabled only when `HIVE_PI_HEARTBEAT=1`, set by the new daemon in `piSpawnEnv` (`internal/agent/settings.go`). A keyless old daemon would re-apply a replay through the timestamp guard and re-raise a cleared wait every 5 s.

### Accepted behavior

- Daemon restart with Pi alive: the fresh `Machine` has no instance, so the first beat applies `lastState`. If that was `turn_end`, the wait comes back once after the restart even if it had been cleared. A restart already resets every session; re-surfacing the last turn end is acceptable.
- A daemon downgrade under a Pi spawned by the new daemon re-raises cleared waits every 5 s until that Pi restarts. Rare; noted in Constraints.
- A straggler from the old instance landing after the new instance's first report, or two live instances both beating (a leaked interval, a nested `pi` inheriting `HIVE_SESSION_ID`), flips `extInstance`. Each flip is an unseen key and re-applies that instance's latest state, which can re-raise a wait the user cleared. The straggler window is one sender timeout (≤ 2 s). Two live instances is misuse; `TestAlternatingInstancesDocumented` pins the behavior.
- Only the latest state is healed; a lost middle `tool_start` in a parallel batch leaves a gap in the activity ring. Wording says "latest state", not "every report".

### Files to change

1. Revert the first approach as a normal commit restoring `origin/main`'s versions: `internal/agentstate/machine.go`, `machine_test.go`, `internal/registry/registry.go`, `state_test.go`, `cmd/hivegui/frontend/src/app/events.ts`, `test/dom/events-focus.test.ts`, `docs/product-specs/336-session-state-model.md`; delete `attention-dwell.ts`, `attention-dwell.test.ts`, `.changesets/stale-pi-turn-raises-attention.md`.
2. `internal/wire/control.go`: `Instance`/`Seq` and doc.
3. `internal/daemon/daemon.go` `applyEventFrame`: refuse half a key.
4. `internal/agentstate/machine.go`: `Event.Instance/Seq`, `extInstance/extSeq`, `Replay`, the keyed `Apply` branch.
5. `internal/registry/registry.go` `ApplyAgentEvent`: replay short-circuit and key pass-through.
6. `internal/agent/settings.go` `piSpawnEnv`: `HIVE_PI_HEARTBEAT=1`.
7. `internal/agent/pi/hive.ts`: instance/seq stamping, `lastState`, `session_start` idle, heartbeat start/stop.
8. `internal/buildinfo/contract.go`: 13 plus history.
9. `docs/design-docs/agent-activity.md`: ordering key and heartbeat paragraph.
10. `docs/design-docs/pi-question-attention-debugging.md`: what the heartbeat heals and what it can't.

### New files

- `.changesets/pi-self-healing-state.md`: `type: fixed`, `bump: patch`, `issue: 423`, `pr: 424`. It mentions that the tooltip will read "reported by the agent" for Pi sessions more often.

### Tests

`internal/agentstate/machine_test.go`:
- `TestReplayIsLivenessOnly`: apply `waiting_input` (A,5), `ClearWaiting`, `Replay`(A,5) is true, state stays `idle`, and the session is still trusted `HookStaleAfter` after the replay's `Now`.
- `TestReplayHealsLostState`: (A,1) `tool_start`, (A,2) `waiting_input` never delivered, then its replay `Replay`=false and `Apply` lands `waiting_input`.
- `TestLostStateNotMaskedByLaterPlan`: (A,3) `waiting_input` lost, an unkeyed `plan` and `ping` delivered, then replay (A,3) applies `waiting_input`.
- `TestOlderSeqSameInstanceIsReplay`: after (A,5), (A,4) is a replay and state is unchanged.
- `TestNewInstanceAccepted`: after (A,50), (B,1) applies; a straggler (A,51) is also accepted, and (B,1)'s replay then applies again (switch back heals).
- `TestReplayAfterHeuristicTakeoverRestoresTier`: (A,2) `turn_end`, `ClearWaiting`, `Output` past `HookStaleAfter` (heuristic working), then replay (A,2): `Replay` handles it, state `idle` (the cleared wait stays cleared) on the extension tier.
- `TestReplayedToolStartAfterTakeoverAddsNoRingEntry`: ring length unchanged.
- `TestKeyedToolEndAndPlanSameAtAppliesPlan`: keyed `tool_end` plus unkeyed `plan` at the same `at` applies the plan.
- `TestHealedOldAtDoesNotMoveOrderAtBack`.
- `TestKeyedSessionStillGoesStale`: last keyed event, then no reports for `HookStaleAfter`, then `Output`, gives heuristic working.
- `TestAlternatingInstancesDocumented`.
- `TestKeyedEventBypassesTimestampGuard`: (A,2) with `At` 1 s older than (A,1)'s applies as a normal state event.
- `TestKeyedEventAfterExitStaysExited`.
- Existing unkeyed ordering tests stay green.

`internal/registry/agent_event_test.go`:
- `TestReplayDoesNotBroadcast`: a subscriber sees one `state` for (A,1) and nothing for a repeat.
- `TestApplyAgentEventHealsLostState`: through `ApplyAgentEvent`, (A,1) then a replay of an unseen (A,2) `waiting_input` sets `NeedsAttention`.
- `TestReplayKeepsExtensionTierTrusted`: replays spaced under `HookStaleAfter` keep `state_source=extension` across a sampled screen change past 30 s.

`internal/daemon` (existing `applyEventFrame` tests):
- `TestEventFrameRefusesHalfKey` and `TestEventFrameRefusesKeyOnHookSource`.

`internal/agent/pi_test.go`:
- Extend the `encodeFrames` decode test for `instance`/`seq`.
- `TestPiSpawnEnvEnablesHeartbeat`.

`internal/agent/pi/hive.test.ts`:
- `state-bearing events get increasing seq; plan and ping are unkeyed`.
- `session_start sends idle`.
- `heartbeat re-sends lastState byte-for-byte` (injectable interval).
- `heartbeat skips while reports are queued` (the in-flight dial is not counted).
- `session_start mid-run sends permission_resolved, not idle`.
- `no heartbeat without HIVE_PI_HEARTBEAT`.
- `heartbeat stops on any session_shutdown, and a new factory run has a fresh instance and its own heartbeat`.

### Verification

- `go build ./... && go vet ./... && staticcheck ./... && go test ./...`: `TestReplayHealsLostState`, `TestLostStateNotMaskedByLaterPlan` and `TestReplayAfterHeuristicTakeoverRestoresTier` are the behavior checks and fail against a single all-kinds mark or a liveness-only replay.
- `node --test internal/agent/pi/`
- `scripts/check-daemon-contract.sh`
- Manual, isolated (`HIVE_SOCKET`/`HIVE_STATE_DIR` in temp dirs), `HIVE_DEBUG_STATE=1` hived: (a) a Pi session asks a question: `-> waiting_input src=extension`; (b) leave an idle Pi for more than 60 s, then type: no `src=heuristic` lines (today this flips to working/heuristic). The heal itself is proven by `TestApplyAgentEventHealsLostState` and the hive.test.ts byte-for-byte replay test. There is no shipped drop switch to prove it end to end.

## Second opinion

Round 1: **revise**, confidence 7. The replay idea is sound, but a single all-kinds mark let a delivered plan/ping mask a lost state event. A liveness-only replay could not win back a session the heuristic tier took. The heartbeat lifecycle across `/new`/`/resume`/fork was unspecified, a `Date.now()` epoch could freeze a session after a clock step, and a `/reload` straggler could strand state.
Disposition: all five applied. Only state-bearing events are keyed. A replay onto a non-extension tier goes through `Apply`. Verified in Pi 0.85.1 that those commands re-run the extension factory, so the heartbeat stops on every shutdown and each instance starts its own. The key is a random instance id with no clock. `session_start` sends `idle`, so a new instance always has state to heal with. Nice-to-haves taken: registry-level heal test, exited test, accepted-behavior notes, changeset tooltip note.
Round 2: **revise**, confidence 6. Open points: the `orderAt` stamp was unspecified (`ev.Now` would re-break #421's plan ordering); re-applying a seen key after takeover re-raised cleared waits and duplicated ring entries; the manual "no tick" check was vacuous after the revert.
Disposition: all applied. `orderAt = max(orderAt, ev.At)`; a takeover replay restores a tracked `extState` (no re-apply); the manual check is replaced. Nice-to-haves taken: `session_start` sends working when mid-run, keys refused off the extension tier, instance flips documented and tested, replay placed after clamping, and a stale test for a keyed session. Not re-reviewed (the loop allows two rounds).

## Decision log

- **2026-09-16** — First approach (stale Pi turn ticks to `waiting_input`, plus a 3 s focus dwell) implemented, then DROPPED at review. Why: operator asked why turns are guessed at all; the tick hid a lost report instead of recovering it, and the dwell produced a working → re-flag → idle flicker on permission prompts.
- **2026-09-17** — Self-healing reports via `(epoch, seq)` plus a 5 s replay of the latest state-bearing event, not a periodic state snapshot. Why: a snapshot re-asserts "waiting" every beat and would undo the user clearing attention; replaying an already-seen key is a no-op, so clears stick.
- **2026-09-17** — Focus dwell dropped from this spec. Why: operator chose the option that drops it; it can return as its own issue.
- **2026-09-17** — Key is (random instance, seq) on state-bearing events only; no clock. Why: round-1 review: a clock epoch can freeze a session after a clock step, and keying plan/ping masks lost state.
- **2026-09-17** — A seen key after heuristic takeover restores a tracked `extState` rather than re-applying the event. Why: round-2 review; re-applying re-raised cleared waits and duplicated ring entries, and restoring only the tier would leave the heuristic's guess trusted forever.
- **2026-09-17** — Heartbeat gated by `HIVE_PI_HEARTBEAT` from the spawning daemon. Why: against an old daemon a keyless replay would re-raise cleared waits every 5 s.

## Progress

- **2026-09-16** — First approach implemented and pushed (c40e6bc); review escalated a flicker; operator rescoped.
- **2026-09-17** — Research for the new approach done; plan drafted.
- **2026-09-17** — Plan approved after two review rounds. First approach reverted (0ba211e7). Implemented: wire key, daemon refusal, machine `Replay`/keyed `Apply`/`extState`, registry short-circuit, `HIVE_PI_HEARTBEAT` spawn env, extension key + heartbeat, contract 13, docs, changeset. Checks: go build/vet/staticcheck/test, node --test (39). Mutation checks: each machine rule (restore on takeover, orderAt from `At` and max'd, clear noted, guard bypass), the registry replay short-circuit and receipt-clock liveness, and the extension's keyed kinds, shutdown stop, `isIdle` and env gate each fail at least one test. The "heartbeat skips while queued" guard has no test: pending() is not observable without a 2 s wedged-daemon fixture.

## Open questions

- Heals only what the extension saw. If Pi never emits `ui_prompt_start`, or the GUI cleared the wait, the debug log is still needed.
- One unix connection per Pi session every 5 s while idle. Cheap, but not zero.

## PR convergence ledger

- **2026-09-16 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 521e10f93880f43b5ef3def59a32bb29b0924be4d025c9c0cf9729571acfde35; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: c40e6bc.
