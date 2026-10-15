# Read Claude's terminal status (OSC 7501) to fix stuck "working" after Esc

- **Spec:** [docs/product-specs/517-read-claude-osc-7501-status-after-esc.md](../../product-specs/517-read-claude-osc-7501-status-after-esc.md)
- **Issue:** #517
- **PR:** #520
- **Branch:** feature/517-osc-7501-status
- **Status:** active

## Summary

`hived` answers Claude's OSC 7501 capability query once per spawn of an agent session, parses the 7501 status Claude then emits, and feeds it to the agent state machine as a new low-trust `osc` tier. The full why is in the spec; this file is the how.

## Research

**PTY read path (`internal/session`)**
- `session.go:285` `readLoop` is the only PTY reader: `ptmx.Read` → `deliver` (:326, holds `s.mu`) → `noteTitle()` → `noteBell(buf[:n])` (:382, locks `s.mu` only to scan and copy the hook, then calls the hook unlocked).
- `bell.go:25-97` `bellScanner` is a five-state machine (text/Esc/CSI/OSC/OSC-Esc) that persists across chunks. It never buffers OSC payload and must see every chunk or it desyncs. A 7501 parser needs a capped payload buffer and is fed from the same call site.
- `Session.Write` (:519) is a bare `ptmx.Write` with no lock. Writing the query reply from the `noteBell` path (after `deliver`, `s.mu` released, on the readLoop goroutine) is safe. Never write inside `deliver` or `EmitAtomicReplay` (both hold `s.mu`). Lock order is one way: registry `r.mu` → `session.mu`.
- `session.Options` (:100) has no agent notion. Gating to agent sessions needs a new field set from `registry/create.go:182`; the revive path is `registry.go:~1501`.
- Replay cannot re-run the scanner: scanning happens only in `readLoop` on live bytes; attach replay (`SubscribeWithAtomicReplay` :476, `EmitAtomicReplay` :512) writes to the client only. A scrollback ring containing `ESC]7501;?` is safe.

**State machine (`internal/agentstate/machine.go`)**
- Sources are consts in `internal/wire/control.go:351-380` (heuristic, hook, extension, laya, acp); wire field `state_source,omitempty` (:265); `registry.go:338` fills it; `sourceName` at `registry.go:705`. Laya is "daemon-only" and the existing fills-gaps-only tier: `Classifiable` (:725) / `Classify` (:746). Event socket (`daemon.go:972`) accepts only hook/extension, so `osc` is daemon-only with no socket change.
- `trusted(now)` (:288) means "reporter alive", not "state current". `lastEventAt` is state-bearing recency. Brain entry `pi-heartbeat-masks-hook-staleness`: test Replay/Bell/Output/stateless-Apply against a heartbeating Pi fixture.
- `Bell` (:361) moves only idle/working → `waiting_input` on every tier, source unchanged. Registry side `noteBell` (`registry.go:582-590`) = `Bell` + `announceStateLocked`. `needsAttention` (`registry.go:300`) is derived from state alone, so mapping blocked → waiting_* gets attention for free.
- Other tier lists needing an `osc` decision: `trusted` (:296), `StaleAt` (:708), `Replay` (:464), `Tick` (:482), `Output` (:325), `wantsUser` (:797).

**Hook-tier interaction (finding that changes the spec's reading)** — a hook-driven Claude turn sets `working` via hooks and Esc fires no hook, so the hook tier is trusted *and* its last event is recent when the user presses Esc. A literal "osc changes nothing while a hook report is fresh" (success criterion 3) would ignore the Esc `idle` and leave criterion 1 unmet. Resolution needed: see Decision log.

**Wire / GUI / contract**
- `DaemonContract` is `internal/buildinfo/contract.go:245` (28 → 29; main was already at 28 by the time of implementation). Do not bump `wire.PROTOCOL_VERSION`. A new `state_source` string value needs no new field/frame; `hivegui/app_*.go`, `hived-ws-bridge`, `testclient` need no struct change.
- GUI: `cmd/hivegui/frontend/src/lib/session-state.ts` (`STATE_SOURCE` :47, inferred helper :52-58, `sourceWords` :111, used :140), `src/app/state.ts:81`, tests `test/unit/session-state.test.ts`, `test/e2e/wails-mock.ts`; also `cmd/hivebar/{client,model}.go`. Today any non-empty non-laya source reads "reported by the agent".

**Tests to copy:** `internal/session/bell_test.go` (chunk-boundary cases), `internal/agentstate/laya_test.go` (:241/:271/:289 tier precedence), `machine_test.go:397`, `internal/registry/state_test.go:415` (real PTY → attention), `registry.go:2135` `NoteBellForTest` (add a twin), `internal/session/testdata/state/*.bin`, `cmd/hived/claude_probe_test.go` (opt-in probes).

**Probe evidence** (`docs/references/agent-osc-signals.md`, "Probe results"): reply `ESC]7501;?ESC\` accepted; startup `idle`; `working`; turn end `done`; question `blocked:kind=question`; Esc → `idle`; exit `clear`. `blocked:kind=permission` unverified.

**Prior lessons:** `pi-heartbeat-masks-hook-staleness` (above). No other brain hits.

**Conventions card:** build `./build.sh`; tests `scripts/test.sh [go|unit|dom|e2e]`; verify under CI toolchain `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`; per-GOOS `staticcheck`+`go vet` (darwin/linux/windows); `biome ci .` (not `biome lint`); `scripts/check-daemon-contract.sh <base> <head>`; Playwright with `CI=1`; shell out only via `proc.Command`; tests with every change; changeset in `.changesets/<slug>.md` (never edit `CHANGELOG.md`); `site/features.json` entry with `since: "Unreleased"`; wire JSON snake_case; verify by exit code not output grep.

## Approach

Two daemon additions, no new wire frame.

1. **Scan + answer (internal/session).** `bellScanner`'s OSC state keeps the first 256 payload bytes (prefix only) and keeps consuming to the terminator (BEL **or** ST; query uses BEL, status uses ST) without storing more. At the terminator it classifies from the prefix: `7501;?` → query; `7501;state=...` → status {state, kind} parsed from the prefix (state and kind precede the long `title`/`msg` fields, so a long question message never drops the report). `Session` gains `Options.AnswerStatusQuery bool` (set by registry for every PTY agent session, `Entry.Agent != ""`, including Codex/Gemini — answering puts bytes on stdin of any agent that sends the query) and a once-per-spawn flag under `s.mu`. On a query with the option armed and flag unset, readLoop sets the flag and writes `ESC]7501;?ESC\` **off the readLoop goroutine** (`go s.ptmx.Write(reply)`) so a full PTY input queue (large paste, a #513 message injection at spawn) can never stall output draining. Statuses go to `SetStatusHook(func(Status))`, called unlocked like the bell hook. Replay never scans.
2. **`osc` tier (internal/agentstate + registry).** `wire.StateSourceOSC = "osc"`, daemon-only. `Machine.OSC(st Status, now) bool`, modelled on `Classify`; never touches `hookSeenAt/orderAt/reportedAt/lastEventAt`. A new `oscAt` records the daemon-clock time of the last osc report.
   - Mapping: working→working; idle→idle; done→waiting_input + `turnEnded` (like hook `turn_end`); blocked kind=permission→waiting_permission; blocked question/auth/none→waiting_input; error→error; clear→ignored.
   - **Gap rule:** applies only when `Classifiable`-style conditions hold (no tier speaking, or the speaking tier silent >30 s); never overrides a user wait, ACP, or Exited.
   - **Interrupt edge (the one exception):** an osc `idle` that follows an osc `working` in the same turn takes a non-ACP `working` state to `idle` even when a hook tier is fresh (hooks cannot report Esc). It never lowers a wait and never raises. It keeps the existing source label (no flip to `osc`, so `isInferredSource` is unaffected and a hook session's plan does not read stale). It records `oscIdleAt`; `Apply` then drops a later working-raising, non-`prompt`, main-thread event whose `ev.At` precedes `oscIdleAt` (a hook process in flight when Esc landed must not resurrect `working`).
   - **Fresh-osc immunity:** while `now - oscAt <= HookStaleAfter`, Laya (`Classifiable` returns false) and byte-level `Output` do not overwrite an osc-sourced state, so the post-Esc repaint ("Interrupted…") cannot flip it back to heuristic `working` and Laya cannot guess over an exact report. When osc goes stale, the state falls through to heuristic/Laya/Tick as for an orphaned tier. `Bell`, hook/extension/ACP `Apply` still take over.
   - `StaleAt` returns `ok=false` for osc (as laya/acp), `Replay` leaves it alone like Laya.
   - `registry.noteOSC` mirrors `noteBell` (`announceStateLocked(e, prev, "osc")`).
3. **GUI.** `sourceWords` gets an `osc` case; `isInferredSource` counts a session whose source is `osc` as inferred. `icons.md` wording rule updated.
4. **Contract/docs.** `DaemonContract` 28→29 with a history line modelled on the laya entry (`contract.go:84-88`: an older GUI mislabels `osc` as "reported by the agent"). Changeset, `site/features.json` entry, `docs/design-docs/agent-activity.md` and `control-plane.md:34-43` (trust-tier table gets an `osc` row), `docs/design-docs/ui/icons.md:50-53`. Spec amended: criterion 3 becomes "while a hook or extension report is fresh, osc changes no state **except** an osc `idle` after an osc `working` ends a hook `working` (the Esc interrupt hooks cannot report)", and the forgery non-goal now says a forged working→idle sequence can end a hook-fresh `working` (never a wait).

Why not GUI-side: absent for headless sessions. Why not `Apply` with source=osc: it moves the tier clocks and would make a forged report look like a heartbeat.

Platform note: Windows ConPTY may swallow unknown OSC; 7501 on Windows is UNVERIFIED and out of the verified scope.

### Files to change
1. `internal/session/bell.go` — prefix-capped OSC capture, BEL/ST terminators, 7501 classification.
2. `internal/session/session.go` — `Options.AnswerStatusQuery`, once flag, async answer, `SetStatusHook`.
3. `internal/registry/create.go`, `registry.go` — option for agent PTY sessions (create + revive), `attachSessionHooks` status hook, `noteOSC`, `sourceName`.
4. `internal/agentstate/machine.go` — `Machine.OSC`, interrupt edge, `oscAt/oscIdleAt`, `Classifiable/Output/StaleAt/Replay/Tick/Apply` treatment.
5. `internal/wire/control.go` — `StateSourceOSC`.
6. `internal/buildinfo/contract.go` — bump to 28 + history.
7. `cmd/hivegui/frontend/src/lib/session-state.ts` (+ `src/app/state.ts` if it lists sources).
8. Docs: `agent-activity.md`, `control-plane.md`, `ui/icons.md`, spec 517 amendment, `.changesets/read-claude-osc-status.md`, `site/features.json`.

### New files
- `internal/session/osc7501_test.go`, `internal/agentstate/osc_test.go`.
- `internal/session/testdata/osc7501/claude.bin` — **recorded**, not hand-built: produced by the opt-in `TestClaudeProbeOSC7501EscGoesIdle` when `HIVE_PROBE_RECORD=<path>` is set (writes only the OSC 7501 sequences from the real PTY stream, keeping real terminators: query BEL, status ST). Includes a long-`msg` question case (>256 B).

### Tests
- Scanner: `TestStatusScannerParsesRecordedClaudeStream` (also split at every byte offset), `TestStatusScannerLongMsgStillParsesStateAndKind`, `TestStatusScannerUnterminatedOSCResyncs`, `TestStatusScannerBELAndSTTerminators`.
- Session: `TestQueryAnsweredOncePerSpawnForAgentSessionsOnly`; `TestQueryAnswerNeverBlocksReadLoop` (fake PTY whose write blocks; assert readLoop keeps delivering); `TestReplayDoesNotRescan` (attach N times: status-hook and query-seen counts unchanged, reply written exactly once in total).
- Machine: `TestOSCFillsGapWhenNoTierSpeaks`, `TestOSCDoesNotOverrideFreshHook`, `...ExtensionOrACP`, `TestOSCIdleEndsHookWorkingAfterOSCWorking`, `TestOSCIdleKeepsHookSourceLabel`, `TestOSCIdleNeverClearsAWait`, `TestLateHookAfterOSCIdleDoesNotResurrectWorking`, `TestLayaDoesNotOverrideFreshOSC`, `TestOutputDoesNotUndoFreshOSCIdle` (hooks off), `TestStaleOSCFallsThroughToHeuristic`, `TestOSCBlockedQuestionSetsAttentionWithHooksOff`, `TestOSCDoneMapsToTurnEnd`, `TestOSCDoesNotMaskStaleHeartbeatingPi`, `TestHookApplyOverwritesOSC`, `TestStaleAtIsNotSetForOSC`.
- Registry: `TestOSCEscReachesIdleThroughTheRealPTY`, `TestOSCQuestionReachesAttentionThroughTheRealPTY` (real PTY script printing the recorded stream, from `state_test.go:415`).
- GUI: `session-state.test.ts` osc wording + inferred cases.
- Opt-in `TestClaudeProbeOSC7501EscGoesIdle` (real Claude; skipped unless enabled, like the other probes).

## Verification
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/session ./internal/agentstate ./internal/registry ./cmd/hived >/dev/null && echo OK`
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... || exit 1; GOOS=$os go vet ./... || exit 1; done` (run under `set -e` in the shell; fish: `or exit 1`)
- `(cd cmd/hivegui/frontend && npm run test:unit && npx biome ci .) >/dev/null && echo OK`
- `scripts/check-daemon-contract.sh origin/main HEAD`; `scripts/test.sh go unit dom`
- Mutation checks (stage the fix first, never `git checkout --`): remove the precedence guard in `Machine.OSC` → `TestOSCDoesNotOverrideFreshHook` fails; drop the once flag → once-per-spawn test fails; remove the `oscIdleAt` drop → `TestLateHookAfterOSCIdleDoesNotResurrectWorking` fails; remove Output immunity → `TestOutputDoesNotUndoFreshOSCIdle` fails; make the answer synchronous → `TestQueryAnswerNeverBlocksReadLoop` fails.
- Real run per `docs/verifying-the-gui-by-hand.md`: `wails dev`, start a Claude session, press Esc mid-turn, read the session row's state at `http://localhost:34115` via a throwaway Playwright script; expect idle within ~1 s.

## Open questions / risks
- Operator sign-off on the spec amendment (criterion 3 interrupt-edge exception; forgery non-goal wording).
- A background/foreground subagent may make Claude emit `idle` mid-run, prematurely dropping a hook `working`. Unobserved; no mitigation planned without probe evidence (a mitigation would break Esc during subagent runs).
- `blocked:kind=permission` unverified.
- Windows (ConPTY) unverified.
- Query armed for all agents, not only Claude (harmless for agents that never ask).

### Revision 3 — round-2 must_fix (supersedes conflicting text above)

- **Interrupted latch replaces the `ev.At < oscIdleAt` compare.** The interrupt edge sets `interrupted`. While set, main-thread non-prompt hook events (tool_end from `PostToolUseFailure`, late `PermissionRequest`/`Notification`) are recorded as activity but cannot move state. Cleared only by `KindPrompt`, an osc `working`, `turn_end`, or Exited. A `lastOSC` field is updated even when the report is not applied (hook fresh); "same turn" = since the last prompt/turn_end. Tests: `TestToolFailureAfterOSCIdleStaysIdle`, `TestLatePermissionRequestAfterOSCIdleStaysIdle`, `TestPromptAfterOSCIdleClearsLatch`. Record "Esc during a running tool" in the opt-in probe/fixture.
- **osc may revise its own waits** (as Laya revises its own): the `wantsUser` guard exempts `source == osc`. Tests: osc `done` → osc `working` → working; osc `blocked` → osc `idle` → idle, hooks off, no `ClearWaiting`.
- **`trusted()` returns false for the osc source**; osc's 30 s immunity is a separate `oscFresh(now)` check. `Tick` is NOT immune: it demotes an osc `working` whose output stopped (cross-check against lost osc `idle`), pinned by `TestTickDemotesOrphanOSCWorking`. `TestOSCDoesNotMaskStaleHeartbeatingPi` asserts the heartbeating-Pi path (hookSeenAt refreshed by Replay) still leaves osc untrusted.
- Nice-to-haves taken: parse fields by key in the prefix (not position); `TestReplayDoesNotRescan` renamed `TestReplayIsStructurallyUnscanned` (documented structural guard, counts-based); unterminated-OSC test asserts "ends at a later terminator, BEL scanning not desynced"; comment that the async reply bypasses `Session.Write` (os.File writes serialize) and swallows post-close `ErrClosed`.

## Second opinion

Independent reviewer (opus subagent), two rounds. Round 1: revise, confidence 7, 7 must_fix, all applied (prefix-capped payload, async reply, Laya/Output immunity, replay test, doc blast radius, fixture provenance, late-hook guard). Round 2: revise, confidence 7, 3 must_fix, all applied in Revision 3 (interrupted latch, osc revises its own waits, `trusted()` false for osc); the result was not re-reviewed because the loop allows one re-review. Operator approved on 2026-10-15 including the spec amendment.

## Decision log

- **2026-10-15** — Found by the live probe, not the plan: (1) a startup `idle` after the SessionStart ping must not relabel a live hook session, and osc keeps a live hook/extension tier's label whenever it supplies a state (`liveTier` rule) — otherwise the existing real-Claude probes' "hook tier" waits flake; (2) `oscWorking` must span a `blocked` report, or Esc on a permission dialog never latches; (3) `blocked:kind=permission` is emitted (resolves the open question); (4) status reports end with BEL, not ST.
- **2026-10-15** — Spec amended with operator approval: criterion 3 gains the interrupt-edge exception; forgery non-goal widened to forged working→idle sequences (never a wait). Why: hooks cannot report Esc, so a literal gap-only rule leaves criterion 1 unmet.
- **2026-10-15** — Brainstorm decisions: Claude-only verified scope; osc tier fills gaps only; forgery accepted as for BEL; criteria and non-goals as in the spec.

## Progress

- **2026-10-15** — Research done; stage → PLAN.
- **2026-10-15** — Plan approved; stage → IMPLEMENT.
- **2026-10-15** — Implemented on `feature/517-osc-7501-status`: scanner + once-per-spawn async reply (`internal/session/osc7501.go`, `bell.go`, `session.go`), `osc` tier (`Machine.OSC`, interrupted latch, own-wait revision, `trusted()` false for osc, Output/Laya shield), registry wiring, GUI wording, `DaemonContract` 29 (main was already at 28), docs, changeset, features.json. Unit + real-PTY tests; mutation-checked (each guard removal fails its test). Live probe (real Claude 2.1.301) passes: Esc mid-turn → idle with the hook label; Esc during a tool stays idle 5 s; permission dialog = `blocked:kind=permission`.

## Open questions

- None open. (`blocked:kind=permission` was verified by the live probe.) Windows ConPTY remains unverified.

## PR convergence ledger
- **2026-10-15 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 4ccc86335635113f09a361b4cbbe8d41c4ea298e4374dbd4c09f60d3e1d8affc; threads_open: 0; action: autofix+push (orchestrator applied both findings as spec-conforming fixes: Esc on a stale tier relabels to osc, plain "shell" never answers the query; plus batched status hook and doc fix); head_sha: c19043ad.
