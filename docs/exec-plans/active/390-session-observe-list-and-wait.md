# Observe from a session: `hived session list` and `hived wait`

- **Spec:** [docs/product-specs/390-session-observe-list-and-wait.md](../../product-specs/390-session-observe-list-and-wait.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../../design-docs/agent-orchestration.md)
- **Issue:** —
- **Branch:** `feature/390-session-observe`
- **PR:** [#525](https://github.com/lucascaro/hive/pull/525)
- **Status:** active

## Summary

Let any `ModeSession` connection list its project's sessions with a
narrow field set, and add `hived wait` that blocks until a sibling
reaches a state. Read-only; no grant. 389 shipped (#524).

## Research

Verified against `main` @ 3b5fc16a, DaemonContract 32.

- `internal/daemon/daemon.go` `serveControl` (~1050): a restricted
  (`ModeSession`) connection **drops every `SESSION_EVENT`**
  (`if restricted { continue }`), so "events already flow" is false.
- Restricted frames are allowlisted in `sessionModeFrames` (~1346);
  `LIST_SESSIONS` is not in it. The connect-time SESSIONS snapshot is
  `ownSessionOnly` (own id + project) and `hived idea list` reads its
  project id from it — it must not change.
- Every read on a restricted conn has a 30 s idle deadline
  (`sessionModeIdleDeadline`, ~1318), re-armed per frame. A wait of
  minutes must not depend on reads. Polling would keep the conn alive
  but leaves the daemon unable to log the spec's evidence line.
- `Registry.SendFromSession` (`internal/registry/deliver.go:394`)
  resolves id-then-exact-name within the caller's project, errors
  `ErrNotFound` / `ErrAmbiguousTarget`. Reuse, do not re-implement.
- `watchSessionExit` (`internal/registry/registry.go:~1641`) calls
  `machine().Exit()` before broadcasting, so `exited` does fire.
- `Registry.Subscribe` (`events.go:26`): a slow listener (64-slot
  buffer) is dropped by closing its channel; `Registry.Close()` closes
  all; Subscribe after Close returns a closed channel.
- States: `wire.State*` (`control.go:~338`); idle is `""`.
- CLI shape: `cmd/hived/msg.go` (`sendMsgFromSession`, `ideaEnv`,
  `ideaDial`, `awaitSent`); dispatch in `cmd/hived/main.go:~40`.
- 389's doc footprint (the template for this one): `.changesets/`,
  `README.md`, `docs/design-docs/agent-orchestration.md`,
  `docs/design-docs/control-plane.md`, `site/features.json`.

### Conventions card

- Build `./build.sh`; tests `scripts/test.sh` (layers go·unit·dom·e2e).
- Verify under the CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`;
  per-GOOS `staticcheck` + `go vet` for darwin/linux/windows.
- Wire change: explicit snake_case tags; update `wire/testclient`;
  new frames do NOT bump `PROTOCOL_VERSION`; daemon-side behaviour
  change bumps `buildinfo.DaemonContract`
  (`scripts/check-daemon-contract.sh <base> <head>`).
- Shell-outs via `internal/proc` only. Verify by exit code, not grep.
- User-visible change: `.changesets/<slug>.md` + `site/features.json`
  entry (`since: "Unreleased"`); never edit `CHANGELOG.md`.

### Prior lessons

None matched (not searched beyond the memory index; no hive-brain hit
was needed for this change).

## Approach (decision: B — daemon-side `WAIT_SESSION`)

Alternative A (client polls `LIST_SESSIONS` at 1 Hz) was rejected: the
daemon never sees a wait finish, so it cannot emit the evidence line.

1. **wire:** `FrameWaitSession` (C→S) / `FrameWaited` (S→C) with
   `WaitSessionReq{session_id, states[], timeout_s, after_working}` and
   `WaitedResp{session_id, state, reached}`; snake_case tags. Add
   `FrameWaitSession` and `FrameListSessions` to `sessionModeFrames`.
2. **List:** restricted `LIST_SESSIONS` returns the caller's project
   sessions as exactly `{id, name, agent, state}` (`ownProjectOnly`).
   Connect-time snapshot stays `ownSessionOnly`.
3. **Wait (daemon, restricted only):** resolve the target in the
   caller's project (shared resolver with `SendFromSession`); reject
   self, missing, other-project and ambiguous targets immediately.
   Validate `states` against the known set (`idle` ⇄ `""`; `gone`
   rejected); default `idle,exited,error`. Order: Subscribe → read
   current state → loop `select` on listener / timer / `ctx.Done()`;
   `defer unsub()` on every path. Closed listener: re-subscribe and
   re-read state via `reg.List()`; a re-subscribe that is immediately
   closed (bounded retries) means "daemon shutting down". Target
   removed → `reached=gone`. Timeout default 10 min, clamped to 30 min.
   Handled inline in the read loop (no `ReadFrame`, so no idle
   deadline while waiting); the write of `WAITED` gets a fresh write
   deadline; a failed write logs `reached=disconnect`.
   `--after-working` (decision b): first wait for the target to leave
   idle, then for a target state — closes the race where `hived wait`
   right after `hived msg` returns on the stale idle.
   Log: `daemon: wait caller=session from=<id> to=<id> reached=<state|timeout|gone|disconnect>`
   (prefix/order mirrors the send line).
4. **Throttle:** the wait arm must not hold a plugin throttle slot for
   its duration — dispatch such that `tag.wait/release` (`daemon.go:~1331`)
   brackets nothing long-lived, and pin it with a test.
5. **CLI:** `hived session list` (tab-separated `id name agent state`;
   exit 2 outside a session); `hived wait <session> [--state a,b]
   [--timeout d] [--after-working]` — exit 0 reached, 1 timeout/gone,
   2 bad args / not in a session / unknown or refused target.
   `--timeout` ≤ 0 or > 30m rejected; `--state gone` rejected. Help
   text: `waiting_input` / `waiting_permission` never time out, so
   `--state idle` on a session at a permission prompt waits until
   `--timeout`. Client conn deadline = timeout + slack.
6. **Housekeeping:** `buildinfo.DaemonContract` 32 → 33.

### Files to change

- `internal/wire/frame.go`, `internal/wire/control.go` (+ frame tables,
  name switch, `ControlFrames`, `ControlEventFrames`),
  `internal/wire/testclient` helper.
- `internal/daemon/daemon.go` (allowlist, list arm, wait arm).
- `internal/registry/deliver.go` (extract the project-scoped resolver).
- `internal/buildinfo/contract.go`.
- `cmd/hived/main.go`.
- Docs: `README.md`, `docs/design-docs/agent-orchestration.md`,
  `docs/design-docs/control-plane.md`, `site/features.json`.
- `cmd/hived-ws-bridge` and `cmd/hivegui`: not needed (session-only frame).

### New files

- `cmd/hived/session.go`, `cmd/hived/wait.go` (+ tests),
  `.changesets/390-session-observe.md` (added, minor).

### Tests

- `internal/wire`: frame-name/table tests; `control_frame_test.go`
  bad-payload list gains `FrameWaitSession` (returns `bad_payload`
  before blocking); check `plugin.FrameCost`.
- `internal/daemon`:
  - `TestRestrictedListSessions_ProjectScopedExactKeys` — other
    project absent; JSON has exactly `id,name,agent,state`.
  - `TestWaitSession_ReturnsOnAgentEvent` — working→idle via
    `AGENT_EVENT`; log line asserted like `TestSessionModeSendLogLines`.
  - `_AlreadyIdleReturnsImmediately`, `_Timeout`,
    `_AfterWorkingIgnoresStaleIdle`, `_ExitedState` (real child exit),
    `_TargetRemovedGone`, `_OtherProjectMissingSelfAmbiguousFailFast`,
    `_SurvivesPastIdleDeadline` (shrunk deadline),
    `_ListenerDroppedResyncs` (flood >64 events),
    `_RegistryCloseMidWait`, `_DisconnectLogged` (closed client conn),
    `_PluginThrottleSlotNotHeld`, and the existing
    restricted-gets-no-events test stays green.
- `cmd/hived` (pattern of `msg_e2e_test.go`): exit codes 0/1/2;
  `--state idle` matches the empty-string state; in a shell session on
  an already-idle sibling `hived wait` returns immediately;
  `--timeout 0` and `--state gone` exit 2.

### Verification

```
export GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)
go test ./internal/wire/... ./internal/daemon/... ./internal/registry/... ./cmd/hived/... >/dev/null && echo OK
for os in darwin linux windows; do GOOS=$os go vet ./... || exit 1; done
scripts/test.sh go >/dev/null && echo OK
# after the 32→33 bump is committed:
scripts/check-daemon-contract.sh origin/main HEAD >/dev/null && echo OK
```

## Second opinion

Independent reviewer (hs-reviewer), two rounds.
- Round 1: **revise**, confidence 7 — slow-subscriber resync, select on
  ctx/timer + unsub, throttle check, self-wait, exact-field projection,
  exit-code-gated verification, frame tables. All applied.
- Round 2: **revise**, confidence 7 — shutdown-vs-drop detection,
  stale-idle race after `msg`, throttle test, write deadline. Applied
  (stale-idle via `--after-working`, per operator approval).

## Decision log

- **2026-09-10** — No grant for read-only verbs. Why: same-user
  processes can already read this from the control socket; a grant
  would add friction and no safety.
- **2026-10-21** — Operator approved: B (daemon-side `WAIT_SESSION`,
  not client polling — the daemon must own the evidence line) and
  option (b) (`--after-working`, since a bare `wait` after `msg`
  returns on stale idle).
- **2026-10-21** — Restricted list projects exactly `{id,name,agent,state}`
  per the spec; `alive`/`state_source`/`project_id` dropped from the
  restricted list. `reached=gone` and `reached=disconnect` extend the
  spec's evidence values — recorded in agent-orchestration.md.
- **2026-10-21** — Wait capped at 30 min (not 1 h): bounds a goroutine
  parked on a client that hung up unnoticed.
- **2026-10-22** — Bumped `go` to 1.27.3 (go.mod and tools/staticcheck/go.mod) inside this PR, at the operator's direction. Why: required `govulncheck` gate failed on GO-2026-6809 (crypto/x509 in the 1.27.2 standard library, fixed in 1.27.3; reachable via `wire.ReadFrame`), which reddens every PR, not just this one. Same precedent as 505.
- **2026-10-22** — Review iteration 2 found the live-state re-check (85e9df88) both fixed a stale-idle bug and introduced a missed-state one, because it second-guessed an ordered event stream. Replaced with an atomic snapshot+subscribe (`Registry.SubscribeSession`): the listener delivers only what happened after the snapshot, in order, so every event is trusted. A per-entry `workCount` (in-memory) lets a re-subscribe after a dropped listener learn that a working→idle cycle went by unobserved, so `--after-working` survives a drop; no wire change. The restricted list reply is its own four-field type (`wire.SessionBrief`) rather than a projected `SessionInfo`, so the keys on the wire are exactly id, name, agent, state.
- **2026-10-22** — Iteration 3: many registry sites build an `Info()` under the lock and broadcast it `updated`/`added` after unlocking, so such an event can be older than the `SubscribeSession` snapshot. A wait therefore settles only on events broadcast under the lock (`state`, `attention`, from `announceStateLocked`) plus any event carrying `exited` (terminal, so never stale). Rejected moving ~12 broadcast sites under the lock, or a per-entry sequence number, as larger than the window warrants.

## Progress

- **2026-09-10** — Plan written. Gated on 389.
- **2026-10-21** — 389 shipped; research verified; plan approved.

## Open questions

- Whether `last_summary` belongs in the list projection. Left out
  until an implementor or the phase-3 gate shows an agent needing it.

## PR convergence ledger

- **2026-10-21 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 8a62b346aad57b08e53edf87d59217f75b2adc4a0dad09999354e199d304c6a5; threads_open: 0; action: autofix+push; head_sha: 593e844a.
- **2026-10-22 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: e952b41f45636f31a59214f715a580d37abe3c0c3175c6ef15787c43091c51ed; threads_open: 0; action: autofix+push; head_sha: 61cf39c0. (Worker pushed 85e9df88; its live-state re-check was replaced in 61cf39c0 after review threads showed it missed reached states; all six threads replied to and resolved.)
- **2026-10-22 iter 3** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: d17cc3647fc03be1bcd6ea35e2f67d29b5d7f383dc2071c1d84bda1de2c14545; threads_open: 1; action: autofix+push; head_sha: 726dd465. (Worker escalated the one IMPORTANT finding as a design call; fixed by the orchestrator in the next commit: settle only on lock-ordered state/attention events plus exited.)
