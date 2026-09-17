# Raise needs-attention when an agent turn goes quiet without reporting its end

- **Spec:** [docs/product-specs/423-raise-attention-when-agent-turn-goes-quiet.md](../../product-specs/423-raise-attention-when-agent-turn-goes-quiet.md)
- **Issue:** #423
- **Status:** active
- **PR:** #424
- **Branch:** feature/423-raise-attention-when-agent-turn-goes-quiet

## Summary

`Machine.Tick` turns a stale, quiet agent turn into plain `idle`, which raises no attention, so a Pi session that stopped on a question can read idle and never call the user back. Make that tick land in `waiting_input` when the working stretch was an agent-opened turn. The root cause of the lost question wait is unreproduced and is handed off in [pi-question-attention-debugging.md](../../design-docs/pi-question-attention-debugging.md).

## Research

### Relevant code

- `internal/agentstate/machine.go` `Tick` (~l.290): the only `working → idle` transition. It fires when `!trusted(now)` and the screen has been quiet for `QuietAfter`, and it sets source `heuristic`.
- `internal/agentstate/machine.go` `Apply` (~l.320): `KindPrompt`, `KindPermissionResolved`, `KindToolStart` and `KindToolEnd` set `working`. `KindTurnEnd`, `KindIdle`, `KindError` and `KindSessionEnd` end the turn. Subagent events (`AgentID != ""`) return before the switch.
- `internal/agentstate/machine.go` `Output`: while stale, a repaint reclaims the session to `working`/heuristic whatever state it came from, including `idle` after a finished turn when the user types. This is why "agent session" alone (`hookSeenAt` non-zero) is not a safe discriminator.
- `internal/registry/registry.go` `announceStateLocked` (~l.553) and `needsAttention` (l.221): attention is derived from state alone, so `waiting_input` raises it on any tier. Only the comment above `announceStateLocked` needs amending.
- `internal/registry/registry.go` `sampleStateLocked` (~l.870): calls `Output` and then `Tick` each sample.
- `internal/agentstate/machine_test.go` `TestStaleTickDemotesTheSourceNotJustTheState` (l.578): asserts `idle` after a stale prompt-opened turn. That expectation changes.
- `docs/product-specs/336-session-state-model.md` l.80–86: the success criterion says `Tick` takes a quiet stale session to `idle`/`heuristic`. Needs an amendment note.

### Constraints / dependencies

- `Snapshot` must stay `==`-comparable, and `New(t0).Snapshot()` must equal the zero `Snapshot`. The new flag lives on `Machine`, not on `Snapshot`.
- The source stays `heuristic` on the tick (PR #344's finding: a tick-derived state must not credit the agent).

### Prior lessons

- No prior lessons matched (brain search `agentstate attention stale tick pi`).

### Conventions card

- Build/lint/test (from CI; AGENTS.md's hivesmith block is still placeholders): `go build ./... && go vet ./... && staticcheck ./... && go test ./...`
- Table/unit tests next to the package; the domain package takes clocks from callers.
- User-visible change: add a `.changesets/*.md` (`type: fixed`, `bump: patch`).
- Comments explain *why*, at the density of the surrounding file.

## Approach

### Daemon: a stale, quiet Pi turn raises attention (Pi only)

Add `turnOpen bool` to `Machine`: "the agent itself started the current working stretch, and nothing has ended it".

- **Set** only for extension-tier events (`ev.Source == wire.StateSourceExtension`; Pi is the only extension reporter, and Claude's hook tier is unchanged), only inside `Apply`'s main-thread switch, after the out-of-order guard (never in `applyLateActivity`, activity.go:172, never for subagent events): `KindPermissionResolved`, `KindToolStart`, `KindToolEnd`. Not `KindPrompt`: Pi posts `prompt` on `input` before model/API-key validation, which can throw with no `agent_start` or `agent_settled` to close the turn. A real run always follows up: Pi's `agent_start` posts `permission_resolved`, Claude's first tool posts `tool_start`.
- **Cleared** by `KindTurnEnd`, `KindIdle`, `KindError`, `KindSessionEnd`; by `Exit`; by `ClearWaiting` on its waiting_input/error → idle branch (the user answered; the turn is over from Hive's view). It is kept on the waiting_permission → working branch, which resumes the turn.
- **`Tick`**: when it fires, if `turnOpen`, set `waiting_input` and clear `turnOpen`; otherwise `idle`. Source is `heuristic` either way.

One-shot: after the tick raises it and the user clears it, repaint-then-quiet stays `idle` until an agent event opens a new turn.

Why not "any session with `hookSeenAt` set": `Output` reclaims a stale finished session to `working` on every keystroke echo, so that would light a session two seconds after the user stops typing in it.

Claude is out of scope (operator decision: it already works). Its hook-tier events never set `turnOpen`, so a stale Claude turn still ticks to `idle`, and its Esc/denied-permission paths are unchanged.

### GUI: dwell on the active Pi session clears attention

New client-driven clear, Pi sessions only (`SessionInfo.agent === 'pi'`): when the active Pi session needs attention and the window has focus continuously for `ATTENTION_DWELL_MS` (3 s), call `clearAttention(id)`. Only the status clears; the terminal is untouched. A waiting_permission clears to working, like a keystroke does.

- `src/app/attention-dwell.ts`: `createAttentionDwell({ delayMs, activeId, hasFocus, needsAttention, clear, setTimer, clearTimer })` returns `{ poke(), cancel() }`. `poke()` arms a timer if the active session is a Pi session, needs attention and the window has focus, and keeps an already-armed timer for the same id instead of restarting it. When the conditions don't hold it cancels any armed timer, so an answer followed by a re-raise restarts the full 3 s. On fire it re-checks all three conditions, so a switch, blur or answer in the meantime makes it a no-op. `cancel()` drops the timer.
- `src/app/events.ts`: one instance. `poke()` from `syncAttentionClass` (state/attention events) and on window `focus`; `cancel()` on window `blur`. Update the comment above the focus listener ("exactly two client-driven clears") and the `noteUserInput` comment ("Window focus is not" a signal). `poke` returns early for sessions without attention, so poking for every session in a `session:list` burst is cheap.
- `src/app/focus.ts` `setActive`: `poke()` after an actual switch is not needed (a switch already clears). Nothing else changes.

### Files to change

1. `internal/agentstate/machine.go`: `turnOpen` field + why-comment; set/clear in `Apply`, `Exit`, `Bell`, `ClearWaiting`; branch in `Tick`; update the `Tick`/`HookStaleAfter` doc comments.
2. `internal/agentstate/machine_test.go`: `TestStaleTickDemotesTheSourceNotJustTheState` uses a hook-tier prompt, so it keeps expecting `idle`; new tests below.
3. `internal/registry/registry.go`: amend the `announceStateLocked` comment (attention now also comes from a stale agent turn and the bell, not only agent reports).
4. `internal/registry/state_test.go`: registry-level test below.
5. `cmd/hivegui/frontend/src/app/events.ts`: wire the dwell and fix the "two client-driven clears" comment.
6. `cmd/hivegui/frontend/test/e2e-real/state-bell.spec.ts`: l.72-74, l.115-116, l.136-137 assert attention survives sitting focused for 2-4 s. Only Pi sessions dwell, so these shell/bell holds should keep passing: verify, and change them only if they use a Pi session.
7. `cmd/hivegui/frontend/test/dom/events-focus.test.ts`: update the "window regaining focus is neither" test's intent and add fake-timer cases: an active Pi session with attention, focus, and 3 s later `SetSessionAttention(id, false)`; the same for a shell session sends nothing.
8. `docs/product-specs/336-session-state-model.md`: amendment note on the stale-tier criterion and on focus not clearing attention, pointing at 423.

### New files

- `cmd/hivegui/frontend/src/app/attention-dwell.ts`: the dwell timer.
- `cmd/hivegui/frontend/test/unit/attention-dwell.test.ts`: its tests.
- `.changesets/stale-agent-turn-raises-attention.md`: `type: fixed`, `bump: patch`, `issue: 423`.
- `docs/design-docs/pi-question-attention-debugging.md`: the debugging handoff (already written).

### Tests

`internal/agentstate/machine_test.go`:

- `TestStaleAgentTurnTicksToWaitingInput`: table over extension-source `permission_resolved`, `tool_start`, `tool_end`. Stale + quiet, `Tick` returns true, `waiting_input`/heuristic.
- `TestStaleTickAfterReportedEndStaysIdle`: table over `turn_end`, `idle`, `error`, `session_end` after a `tool_start`, then `ClearWaiting` (load-bearing for `turn_end`, which lands in waiting_input; say so in the test), stale `Output` and a quiet `Tick`: `idle` (`exited` for session_end).
- `TestHookTierStaleTurnStillTicksIdle`: Claude-shaped `tool_start` with source `hook`, stale + quiet `Tick`: `idle` (unchanged behavior).
- `TestPromptAloneDoesNotOpenATurn`: `prompt` only, stale + quiet `Tick`: `idle`.
- `TestTypingIntoStaleAgentSessionDoesNotRaiseAttention`: `prompt`, `idle`, stale `Output`, `Tick`: `idle`.
- `TestStaleTurnAttentionFiresOnce`: tick raises; `ClearWaiting`, `Output`, `Tick` give `idle`; a new `permission_resolved` re-arms it.
- `TestAnsweredWaitClosesTheTurn`: `tool_start` → `waiting_input` (a declined question) → `ClearWaiting` → stale `Output` → `Tick`: `idle`. (No Bell clear: waiting_input is only left through ClearWaiting or an agent event, so clearing on Bell would be dead logic.)
- `TestAnsweredPermissionKeepsTheTurnOpen`: `tool_start` → `waiting_permission` → `ClearWaiting` (working), stale + quiet `Tick`: `waiting_input`. Pins the accepted denied-permission behavior.
- `TestLateEventDoesNotReopenTheTurn`: `tool_start` → `turn_end` at t+2s → `tool_start` stamped t+1s (goes through `applyLateActivity`) → `ClearWaiting` → stale `Output` → `Tick`: `idle`.
- `TestSubagentEventDoesNotOpenATurn`: `turn_end`, then a subagent `tool_start`, `ClearWaiting`, stale `Output`, `Tick`: `idle`.
- `TestHeuristicTier` (existing) stays green: a plain shell still ticks `working` → `idle`.

`internal/registry/state_test.go`:

- `TestStaleAgentTurnRaisesNeedsAttention`: a live session, `ApplyAgentEvent(tool_start)` with `at` well in the past, `sample` past `QuietAfter`: `Info().State == waiting_input` and `NeedsAttention` true.

`cmd/hivegui/frontend/test/unit/attention-dwell.test.ts` (vitest fake timers):

- clears after `delayMs` when the active session needs attention and the window has focus;
- does not clear before `delayMs`;
- does nothing without focus, or when the session doesn't need attention;
- `cancel()` (blur) before `delayMs` prevents the clear;
- a switch of the active id before firing prevents the clear;
- does nothing for a non-Pi (Claude or shell) active session;
- a second `poke()` for the same id does not restart the timer;
- an answer before firing (poke with no attention) cancels it, and a re-raise restarts the full delay.

### Verification

- `go test ./internal/agentstate/ ./internal/registry/ -run 'StaleAgentTurn|StaleTickAfterReportedEnd|TypingIntoStale|StaleTurnAttentionFiresOnce|AnsweredWait|AnsweredPermission|LateEventDoesNotReopen|SubagentEventDoesNotOpenATurn|StaleTickDemotes|HeuristicTier' -v`. On today's code `TestStaleAgentTurnTicksToWaitingInput`, `TestStaleAgentTurnRaisesNeedsAttention` fail (they read `idle`).
- `go build ./... && go vet ./... && staticcheck ./... && go test ./...`
- `cd cmd/hivegui/frontend && npx vitest run test/unit/attention-dwell.test.ts && npm test && npx biome ci . && npm run typecheck` (after `./scripts/ci-bootstrap.sh` if the wails bindings are missing).
- `cd cmd/hivegui/frontend && CI=1 npx playwright test --config=playwright.real.config.js test/e2e-real/state-bell.spec.ts` with isolated `HIVE_SOCKET`/`HIVE_STATE_DIR`.

## Second opinion

Round 1: **revise**, confidence 7. The flag leaked through paths that end a turn with no end event: ClearWaiting from a declined question, a bell, Claude's Esc, a denied permission. The guard/late-activity placement was untested, and the `announceStateLocked` comment went stale.
Disposition: applied the ClearWaiting/Bell clears, the late-event test, the registry comment amendment, the `session_end` row and the registry-level test. Claude Esc and denied permission are taken to the operator: keep all agents and accept them, mitigated by the new dwell-clear (operator decision).

Round 2: **revise**, confidence 7. The `turnOpen` tracing was clean. Findings: the dwell breaks `e2e-real/state-bell.spec.ts` and contradicts `dom/events-focus.test.ts`; a stale armed timer could clear a re-raised attention early; the Bell clear was dead logic with a vacuous test; `prompt` could open a turn nothing closes.
Disposition: all applied. Checked Pi 0.85.1: built-in and extension slash commands never emit `input` (interactive-mode.js onSubmit, agent-session.js `prompt`), but `input` fires before model validation, so `prompt` no longer opens a turn. Bell clear dropped. Not re-reviewed (one revise round per the loop).

## Decision log

- **2026-09-16** — Attention only for agent sessions, not every working→idle. Why: operator choice. Shells would ping on every command, and agent-reported `idle` (Esc, `/new`) is deliberately quiet.
- **2026-09-16** — Ship the stale-turn fallback without the root-cause fix. Why: the lost `waiting_input` is unreproduced (no patches without a reproducer); the handoff doc carries the diagnosis.
- **2026-09-16** — Keep source `heuristic` on the tick-raised wait. Why: the agent did not report it; the tooltip must say "guessed from terminal output" (PR #344).

- **2026-09-16** — Rule covers all agents, Claude included. SUPERSEDED by the next entry.
- **2026-09-16** — Pi only, for both the stale-tick rule and the dwell. Why: operator: "claude works well already".
- **2026-09-16** — Active + focused for 3 s clears attention. Why: operator: "no question is lost, only the status". Reverses the 336 decision that focus alone never clears; the terminal keeps the question.

## Progress

- **2026-09-16** — Research done, plan drafted.
- **2026-09-16** — Plan approved (Pi only), implementation started.
- **2026-09-16** — Implemented. Go build/vet/staticcheck/test, vitest (1334), biome ci, tsc and e2e-real state-bell (4) all green. Mutation checks: disabling the extension-tier open fails 6 machine tests and the registry test; dropping the ClearWaiting close fails TestAnsweredWaitClosesTheTurn; dropping the Pi check in the dwell fails the dom test.

## Open questions

- A permission prompt the user is looking at for 3 s clears to working while the dialog is still up (same as a keystroke today).
- A permission dialog left open under watch: the dwell clears it to working at 3 s, and about 32 s later the tick raises attention and the dwell clears it to idle, all while the dialog is still up. One extra flash.
- A single silent tool call longer than ~32 s with a fully static screen will now raise attention mid-turn, cleared by its `tool_end`. Pi and Claude animate a spinner while a tool runs, so the screen is not static in practice.
