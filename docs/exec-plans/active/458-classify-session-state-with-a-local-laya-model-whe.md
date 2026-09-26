# Classify session state with a local Laya model when hooks can't tell

- **Spec:** [docs/product-specs/458-classify-session-state-with-a-local-laya-model-whe.md](../../product-specs/458-classify-session-state-with-a-local-laya-model-whe.md)
- **Issue:** — (local-only)
- **Status:** active
- **PR:** #464
- **Branch:** feature/458-laya-state-classifier

## Summary

Add an opt-in classifier that asks a user-run Laya server to label a session's visible screen as one of the existing states, for sessions without trusted hooks (heuristic tier, or hook tier gone stale). The why lives in the spec; this file is the how.

## Research

### Relevant code

- `internal/agentstate/machine.go` — `Machine` (:176) holds `state`, `source`, `hookSeenAt`, `orderAt`, `reportedAt`. Feeders: `Output` :263, `Bell` :302, `Exit` :315, `ClearWaiting` :335, `Replay` :377, `Tick` :404, `Apply` :426. `trusted(now)` :249 = non-heuristic source and `now-hookSeenAt <= HookStaleAfter` (30s, :58). `wantsUser()` :630: waiting/error states are cleared only by ClearWaiting, an agent event or Exit. `StaleAt()` :613. No internal clock; callers pass `now`.
- Source consts: `internal/wire/control.go:324-341` (`StateSourceHeuristic=""`, `"hook"`, `"extension"`, "increasing order of trust"). `SessionInfo.StateSource` (:244, `state_source`) already reaches the GUI via `Entry.Info()` `registry.go:266`. `AgentEvent.Source` (:529) accepts only hook/extension from reporters — "laya" must stay daemon-internal.
- `internal/registry/registry.go` — `Entry.state` :162, `e.machine()` :225 (under `r.mu`), `attachSessionHooks` :483, `ApplyAgentEvent` :539, `noteBell` :510, `announceStateLocked` :643, `needsAttention` :256 (purely state-based; comment :634-642 says the heuristic tier never raises attention). `tickStates` goroutine started in `Open` :1005, 500ms ticker :997, `sampleStateLocked` :1031 compares `ScreenDigest()`. Lock order `r.mu` → `session.mu`.
- Screen text: no plain-text accessor exists. `VT.ScreenDigest()` `internal/session/vt.go:1023`, `Session.ScreenDigest()` `session.go:656`. vt10x `State.String()` returns visible rows joined by `\n`; wrap as `VT.ScreenText()` under `v.mu`. Wide-char continuation cells (spec 142) and trailing spaces need trimming.
- Settings: `internal/agent/settings.go` — `Settings` :39, `settingsFile` :76 (pointer fields = defaults), `resolve()` :88, `LoadSettings()` :122, `SaveSettings()` :142 (atomic), file `agent-settings.json`. GUI writes it directly: `cmd/hivegui/app_calls.go:111-152` (`AgentSettings` mirror, `Get/SaveAgentSettings`), `frontend/src/components/modals/Settings.tsx` tabs :104-113. No hot reload; precedent `internal/daemon/planreview.go:75` re-reads `LoadSettings()` per use.
- GUI source rendering: `frontend/src/lib/session-state.ts:89-121` `stateTooltip`/`sourceWords` (truthiness test — "laya" would read as "reported by the agent"). Non-heuristic-means-live assumptions: `SessionRow.tsx:51,146`, `activity/shared.tsx:38`, `lib/activity.ts:158` `isStale`. Mock: `test/e2e/wails-mock.ts:1682`.
- HTTP: daemon has no HTTP client. GUI precedent `cmd/hivegui/update.go:189-197` (`context.WithTimeout` + `NewRequestWithContext`).

### Laya server (github.com/NandhaKishorM/laya, checked 2026-09-25)

- `pip install "laya[serve]"; laya-serve` → `0.0.0.0:8000`; Docker is build-from-repo only (no published image), compose binds `127.0.0.1:8000`. Runs on macOS (MPS), Linux, Windows (pip); Docker Linux amd64/arm64 CPU. Apache-2.0.
- `POST /v1/systemone` `{"state": <string|object>, "questions": {"<name>": {"type":"choice","instructions":"…","criteria":{"<label>":"<desc>",…}}}}` → `{"answers":{"<name>":{"choice":"…","probabilities":{…},"confidence":…}}, …}`. `GET /health`. Optional `LAYA_API_KEY` Bearer. 503 when >16 concurrent.
- English checkpoint: **512-token context, silently truncated to the first window** — a terminal's tail (the prompt) would be dropped. Must send the tail ourselves.
- CPU latency ~290–600 ms per question warm; single inference worker thread; inter-op threads must be pinned to 1 or latency is ~9 s.
- **Accuracy caveat:** base checkpoints are near chance zero-shot on the typed-decisions benchmark (0.36); probabilities over-confident until temperature-fit; option order matters; use opaque labels; terminal text is out of distribution and untested.

### Constraints / dependencies

- Never make the HTTP call under `r.mu` (blocks every registry op). Snapshot under lock → call unlocked → re-take lock, re-check entry, apply.
- A Laya write must not touch `hookSeenAt`/`orderAt`/`reportedAt`, or a stale hook tier would read as trusted again.
- Laya-produced waiting/error states would raise `needsAttention` and desktop notifications, unlike today's heuristic tier.
- Throughput: 10 sessions × ~0.4 s serial on CPU vs the spec's 3 s latency bar — classify only on screen change + quiet, not every tick.
- Daemon-side behaviour change → bump `buildinfo.DaemonContract`.

### Prior lessons

- (heal-state-reports-by-replaying-keyed-edges) Stacked timeout guesses flicker; heartbeating state snapshots re-asserts "waiting" and undoes the user's clear. Emit a Laya result only when the classified label changes.
- (hook-events-invert-judge-order-per-entity) Keep the liveness clock separate from ordering; a non-hook writer must not advance `hookSeenAt`.

### Conventions card

- Build: `./build.sh` (macOS). Test: `scripts/test.sh [go|unit|dom|e2e]`; e2e-real: `npm run test:e2e:real` (must isolate HIVE_SOCKET + HIVE_STATE_DIR).
- CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`. Static analysis per GOOS: `for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done`. Frontend: `biome ci .`, `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree).
- TDD: every behaviour change ships its test; Go tests beside source, frontend tests under `cmd/hivegui/frontend/test/`.
- Wire JSON snake_case; JS reads `snake_case ?? camelCase`. Daemon-side behaviour change bumps `buildinfo.DaemonContract` (`scripts/check-daemon-contract.sh`).
- User-visible change: `.changesets/<slug>.md` + `site/features.json` entry with `since: "Unreleased"`; update README/DESIGN.md when structural. Validate CSS/UI in a real browser (Playwright Wails mock).

## Approach

A new daemon-side **Laya classifier loop** in the registry, fed by a pluggable `classify` function the daemon wires to a small HTTP client (`internal/laya`). It labels a session's visible screen with one of `working / idle / waiting_input / waiting_permission / error`, and the result enters the state machine through a new `Machine.Classify(state, now)` feeder with a new source `wire.StateSourceLaya = "laya"`.

When a session gets classified (all must hold):
- The feature is enabled. Settings are read through an mtime-checked cache of `agent-settings.json`, so toggling is live without disk I/O on every cycle.
- The session is alive and **classifiable**, via the new `Machine.Classifiable(now)`. That means one of two things:
  - The tier is not trusted: heuristic, laya, or `trusted(now)` is false.
  - Or it is a hook/extension tier whose last **non-replay** event is older than `HookStaleAfter` (30 s).

  This is a new `lastEventAt` clock, set only in `Apply`. `hookSeenAt` cannot be the measure, because Pi's heartbeat (`HIVE_PI_HEARTBEAT=1`, `buildinfo/contract.go:60`; `pi/hive.ts:88,466`) calls `Replay` every 5 s, and `Replay` refreshes `hookSeenAt` at `machine.go:388`. Measured that way, a live Pi would never go stale, and spec criterion 3 could not be met. The staleness threshold N is therefore 30 s of no *new* agent event.
- **Either** the screen digest differs from the digest at this session's last classification attempt and the screen has been quiet for `ClassifyQuietAfter` (1 s), **or** the current state is laya-sourced `working` and older than `LayaRecheckAfter` (30 s). The recheck is what keeps Laya's "working" from pinning forever on a static screen, such as a silent `npm test`. It only fires for laya-working, so idle and waiting screens are never polled. A streaming agent keeps changing the digest and is never sent.
- The current state is not a `wantsUser` state. Waits and errors stand until ClearWaiting, an agent event or Exit, the same rule `Output` follows.

**Cadence and budget.** One classifier goroutine wakes every 500 ms, the same as `tickStates`. The deadline is roughly 1 s of quiet, plus up to 0.5 s of wake latency, plus about 0.5 s of inference, which comes to about 2 s and is under 3 s. Within a cycle the most recently changed screen goes first.

**Lock-safe cycle.** `classifyCycle(ctx, now)` is a separate method so tests can drive it deterministically, the same way `sample()` works in `state_test.go:77`. It runs in three steps:
1. Under `r.mu`, snapshot the candidates as (id, digest, `Snapshot()`, screen tail).
2. Release the lock and call Laya sequentially, each call with a 1.5 s timeout derived from `ctx`.
3. Re-take `r.mu`. Re-check that the entry and the session still exist, **and that the digest *and* the machine snapshot (state, source) are unchanged**. This drops a reply that lands after `Tick`, `Output`, a hook or `ClearWaiting` moved the session in the meantime, so no apply-after-Tick flicker. Then call `Classify` + `announceStateLocked(e, prev, "laya")`.

A result is applied only when the label differs from the current state, following the brain lesson "never heartbeat a state".

**Errors and backoff.** Any error (disabled, connection refused, timeout, non-200, bad JSON, unknown label) is handled the same way:
- It is logged at debug level.
- The state is left untouched, so behaviour matches today.
- The screen's digest is still recorded as attempted, so that screen is not retried.
- An endpoint backoff starts: the next attempt waits 2 s, doubling up to 60 s, and resets on success.

A dead server therefore costs at most one timed-out call per backoff window, never one per session per cycle. The cost is that a screen which failed once is not classified until it changes. That is acceptable, because the heuristic state still stands.

**Shutdown.** The goroutine has its own `classifyCancel` context and `classifyDone` channel. `Close` cancels the context and waits on `classifyDone` *before* taking `r.mu` to close listeners, so an in-flight HTTP call is aborted and can never re-lock and broadcast after `Close`.

**Machine rules.** `Machine.Classify` changes the state and sets `source = laya`. It never touches `hookSeenAt`, `orderAt`, `reportedAt` or `lastEventAt`, so a returning hook takes over through `Apply` as usual. Three existing feeders change:
- `Tick` does not time out a laya-sourced state; `LayaRecheckAfter` bounds it instead. Without this guard Laya "working" would flip to idle and back (brain lesson: stacked guesses flicker).
- `Replay` (`machine.go:393-397`) no longer restores `extState` over a laya-sourced state. It only refreshes liveness. A heartbeat repeats an old report, and letting it re-assert the stale "working" that Laya corrected would flicker every 5 s. The next real `Apply` restores the extension tier.
- `Output` is **unchanged**:
  - On an untrusted laya session (`hookSeenAt` zero, for example Aider), a screen change demotes it to heuristic working. That is correct: the screen moved.
  - On a heartbeating Pi that Laya has overridden, `trusted()` is still true, so `Output` only stamps `lastOutputAt`. The digest gate then re-classifies. The state never passes through heuristic, which is what would let the next `Replay` restore the stale `extState`.
- `StaleAt()` returns ok=false for a laya source. Laya has no report deadline.
- `lastEventAt` is advanced only by state-bearing `Apply` events. `KindPing` and `Replay` heartbeats do **not** advance it: they prove the reporter is alive, not that its state is current. This is how the spec's "while hooks are firing" is defined; see open question 1b.
- **Recheck clock.** The registry stamps `Entry.layaCheckedAt` on **every** attempt: the same label, a different label, or an error. The `LayaRecheckAfter` gate keys on that stamp, not on when a label was applied, so a static laya-working screen costs exactly one call per 30 s.

Laya-produced waits and errors raise `needsAttention` and notifications, per the operator's decision. The registry comment at `:634-642` is updated to say so.

The request is a single `choice` question. It uses **opaque labels** `s1`..`s5` with descriptive criteria (research: option order and boolean-word labels bias the model), and the state is the **tail** of the screen text. Blank lines are dropped and it is capped at 1200 chars, because the 512-token context silently truncates to the *first* window and the prompt lives at the bottom. An optional `model` setting names a fine-tuned checkpoint, which is how the operator's "fine-tune later" plugs in with no code change.

Why this beats the obvious alternative (calling Laya on every 500 ms tick from `sampleStateLocked`): that makes an HTTP call under `r.mu` or on every tick. The first blocks the whole registry. The second overruns a single-worker CPU server with 10 sessions.

Corpus and eval:
- `HIVE_LAYA_CAPTURE_DIR=<dir>` makes the daemon also write each classified screen tail with Laya's label to `<dir>/<agent>-<ts>.txt`.
- The operator corrects the labels and moves the files into `internal/laya/testdata/corpus/<agent>/<state>/`.
- Before commit, `scripts/laya-corpus-scrub.sh` runs deterministic regex redaction (tokens, keys, emails, home paths). Then an LLM review pass reads each capture and flags anything still sensitive; the operator chose "auto review by LLM", and this is a documented process step, not product code.
- A CI test enforces coverage and runs a secret-pattern scan over the committed corpus.
- An env-gated eval test (`HIVE_LAYA_URL`) scores a real server against the corpus and fails below the bar.

### Files to change

1. `internal/wire/control.go` — add `StateSourceLaya = "laya"` beside the other source consts and document that it is daemon-only. `internal/daemon/daemon.go:845` already rejects any source other than hook/extension; the plan adds a test there, not in wire.
2. `internal/agentstate/machine.go`:
   - New `lastEventAt`, set in `Apply` only.
   - New `Classify(s State, now time.Time) bool`. It refuses states that are not classifiable, exited states, `wantsUser` states, and anything that is not one of the five states.
   - New `Classifiable(now) bool`: `!wantsUser && !exited && (!trusted(now) || now-lastEventAt > HookStaleAfter)`.
   - New `ClassifyQuietAfter = 1s`, `LayaRecheckAfter = 30s`.
   - New `QuietFor(now)` accessor and `ClassifiedAt` (when the laya label was set).
   - `Tick` skips `source == laya`.
   - `Replay` does not restore `extState` over a laya source.
   - `StaleAt` returns ok=false for laya.
   - `Output` is unchanged (see Machine rules).
   - `Apply` advances `lastEventAt` except for `KindPing`.
3. `internal/session/vt.go` + `internal/session/session.go` — `VT.ScreenText()` returns the visible rows via vt10x `String()` under `v.mu`, with trailing spaces trimmed, wide-char continuation cells collapsed, and a trailing blank rows drop. Add the `Session.ScreenText()` passthrough.
4. `internal/registry/registry.go`:
   - `classify func(ctx context.Context, screen string) (wire.State, error)` field plus `SetClassifier`.
   - `Entry.layaDigest` (the digest at the last classification attempt).
   - `Entry.layaCheckedAt`.
   - Backoff state.
   - A `classifyStates()` goroutine started in `Open`, with its own `classifyCancel` and `classifyDone`, that calls `classifyCycle(ctx, now)` every 500 ms.
   - `Close` cancels and waits (see Shutdown), and nil-guards both fields for a literally-built `Registry`, the same as the `tickStop` guard at :1807.
   - Update the `announceStateLocked`/`needsAttention` comment at :634-642 to say that Laya states raise attention. The heuristic tier still does not, so `TestHeuristicIdleDoesNotRaiseAttention` (`state_test.go:251`) is kept unchanged.
   - Capture-dir write when `HIVE_LAYA_CAPTURE_DIR` is set: dir 0700, files 0600.
5. `internal/agent/settings.go` — `LayaEnabled bool` (default false), `LayaURL string` (default `http://127.0.0.1:8000`), `LayaModel string` (default ""), added to `Settings`, `settingsFile` (pointer fields) and `resolve()`.
6. `internal/daemon/` (where the registry is opened) — `SetClassifier`. The closure calls `agent.LoadSettings()`, returns `laya.ErrDisabled` when off, and otherwise calls `laya.Classify`.
7. `internal/buildinfo/contract.go` — bump `DaemonContract`.
8. `cmd/hivegui/app_calls.go` — mirror the three fields in `AgentSettings`. Add `TestLayaConnection(url string) string`: GET `<url>/health` with a 2 s timeout, returning "" when OK or an error message.
9. `cmd/hivegui/frontend/src/lib/session-state.ts` — `sourceWords`: `laya` → "classified from the screen by Laya". Keep the truthiness default for unknown tiers.
10. `cmd/hivegui/frontend/src/components/SessionRow.tsx:51,146`, `components/activity/shared.tsx:38`, `lib/activity.ts:158` — treat `laya` like heuristic for "stale/live tier" purposes, because Laya reports no plan or `staleAt`. Factor a single `isInferredSource(src)` helper in `session-state.ts` and use it at all four sites.
11. `cmd/hivegui/frontend/src/components/modals/Settings.tsx` — an "Agent state detection (Laya)" section in the Agents tab with:
    - an enable toggle
    - a URL input, with an inline warning when the host is not loopback ("screen text will be sent to this host")
    - an optional model input
    - a "Test connection" button that shows ok or the error inline
12. `cmd/hivegui/frontend/src/bridge.ts` + `test/e2e/wails-mock.ts` — the new fields and method.
13. `DESIGN.md` — the new `internal/laya` package and the classifier loop in the registry, plus the daemon's first outbound HTTP.
14. `README.md` — a short "Laya state detection (optional)" section: run `laya-serve` (pip or Docker), enable it in Settings, and a privacy note.
15. `internal/wire/phase_frontend_test.go` — extend `TestStateConstantsMatchFrontend` (:50), or add a sibling test, so `StateSourceLaya` must appear in `session-state.ts`.
16. `.changesets/458-laya-state-detection.md` (minor) + a `site/features.json` entry with `since: "Unreleased"`.

### New files

- `internal/laya/laya.go` — `Classify(ctx, client, url, model, screen string) (wire.State, float64, error)`:
  - builds the `/v1/systemone` request (opaque labels, tail ≤1200 chars)
  - reads at most 64 KiB of the response via `io.LimitReader`
  - maps the choice back to a state and rejects unknown labels
  - `ErrDisabled`
  - `Tail(screen string, max int) string`
- `internal/laya/laya_test.go` — `httptest`-based unit tests.
- `internal/laya/corpus_test.go` — the coverage test, the secret scan, and the env-gated eval.
- `internal/laya/testdata/corpus/README.md` — the capture → scrub → LLM review → label process, and the label definitions.
- `internal/laya/testdata/corpus/<agent>/<state>/*.txt` — the captured corpus (aider, codex, shell, pi × 5 states).
- `scripts/laya-corpus-scrub.sh` — the deterministic redaction pass.
- `docs/design-docs/laya-state-classifier.md` — design rationale: gating, trust rules, why opaque labels and a tail, fine-tune hook.

### Tests

Go:
- `internal/agentstate/machine_test.go`:
  - `TestClassifyAppliesOnHeuristicTier`: heuristic idle, Classify(waiting_input), gives waiting_input/laya.
  - `TestClassifyIgnoredWhileHookTrusted`: a hook event at t0, Classify at t0+10s, no change.
  - `TestClassifyOverridesStaleHook`: hook "working" at t0, Classify(waiting_input) at t0+31s, gives waiting_input/laya.
  - `TestClassifyDoesNotRefreshHookClock`: after Classify, `StaleAt`/`hookSeenAt` are unchanged, and a later hook Apply wins.
  - `TestClassifyRespectsWantsUser`: waiting_permission, Classify(working), no change.
  - `TestClassifyRejectsExitedAndUnknown`.
  - `TestTickLeavesLayaStateAlone`: laya working, Tick after 5 s, still working.
  - `TestOutputDemotesLayaToHeuristic`.
  - `TestReplayDoesNotRestoreOverLaya`: Pi extension "working", heartbeat Replays keep coming, event-stale at 31 s, Classify(waiting_input), another Replay, still waiting_input/laya.
  - `TestClassifiableWithHeartbeatButNoEvents`: Apply at t0, Replay every 5 s until t0+31s, `Classifiable` is true; `trusted` is still true.
  - `TestClassifiableFalseWhileEventsFresh`.
  - `TestApplyAfterLayaRestoresExtension`.
  - `TestStaleAtFalseForLaya`.
  - `TestOutputThenHeartbeatDoesNotRestoreStaleExtState`: Pi laya waiting_input, then Output, then Replay, and the source is still laya, not extension.
  - `TestPingDoesNotAdvanceLastEventAt`.
- `internal/session/vt_test.go`:
  - `TestScreenTextPlainRows`
  - `TestScreenTextWideCharsNotDoubled`
  - `TestScreenTextTrimsTrailingBlank`
- `internal/laya/laya_test.go`:
  - `TestClassifyRequestShape`: the server asserts opaque labels, a single choice question, the tail and the `model` field.
  - `TestClassifyMapsChoiceToState` (table over the five labels).
  - `TestClassifyUnknownLabelIsError`
  - `TestClassifyNon200IsError`
  - `TestClassifyTimeout`: the server sleeps past the ctx deadline, and it returns an error within the deadline.
  - `TestClassifyResponseCapped`: an oversized body gives an error, not an OOM.
  - `TestTailKeepsBottom`
- `internal/registry/classify_test.go`. Deterministic: `manualClock` + a new `stopClassifier` helper, and `classifyCycle(ctx, now)` driven with chosen times and a fake classifier.
  - `TestClassifierRunsOnceAfterQuiet`: exactly one call, and the state is set with announce reason "laya".
  - `TestClassifierAppliesWithin3s`: screen change at t0, cycles at t0+0.5 s… The state is applied by t0+1.5 s with an immediate fake. This covers spec criterion 2 deterministically.
  - `TestClassifierNotCalledWhileStreaming`: the digest changes before every cycle, giving 0 calls.
  - `TestClassifierNotCalledForTrustedHook`
  - `TestClassifierCalledForHeartbeatingButEventStalePi`: registry-level. It sends Pi events with a heartbeat through `ApplyAgentEvent`.
  - `TestClassifierRechecksLayaWorkingAfterTTL`, and `TestClassifierDoesNotRecheckIdleOrWaiting`.
  - `TestClassifierErrorKeepsHeuristic`
  - `TestClassifierErrorDoesNotHammer`: the fake always errors, and the screen **changes before every cycle** over 10 s. Calls land only at the backoff edges (t≈0, 2 s, 6 s), which exercises the backoff itself and not just the digest record.
  - `TestClassifierSameScreenNotRetriedAfterError`
  - `TestClassifierBackoffResetsOnSuccess`: after a success, the next changed screen is called in the very next cycle.
  - `TestClassifierLayaWorkingRecheckOncePerWindow`: static screen, the fake always answers working, 120 s of cycles, exactly 1 + 4 calls.
  - `TestClassifierDisabledMakesNoCalls`
  - `TestClassifierDropsResultIfScreenChanged`
  - `TestClassifierDropsResultIfStateMovedMeanwhile`: Tick or ClearWaiting runs between the snapshot and the apply, so nothing is applied.
  - `TestClassifierNotHeldUnderLock`: the fake blocks, and a concurrent `List()` still returns.
  - `TestCloseCancelsInflightClassify`: the fake blocks on ctx, `Close` returns, and there is no broadcast afterwards.
  - `TestLayaWaitingRaisesAttention`
- `internal/agent/settings_test.go`:
  - `TestLayaSettingsDefaults` (disabled, localhost URL)
  - `TestLayaSettingsRoundTrip`
- `internal/daemon/event_mode_test.go`:
  - `TestEventModeRejectsLayaSource`
- `internal/wire/phase_frontend_test.go`:
  - The source-constant parity with `session-state.ts`.
- `internal/laya/corpus_test.go`:
  - `TestCorpusCoverage`: each of aider/codex/shell/pi has at least 1 file per state (runs in CI).
  - `TestCorpusHasNoSecrets`: a regex scan (runs in CI).
  - `TestCorpusAccuracy`: skipped unless `HIVE_LAYA_URL` is set. Fails when overall accuracy is below 0.90 or waiting recall is below 0.95, and prints the confusion matrix.

Frontend:
- `test/unit/session-state.test.ts`:
  - `stateTooltip names Laya as the source`
  - `isInferredSource treats laya like heuristic`
- `test/dom/settings.test.tsx`:
  - `Laya section saves enabled/url/model`
  - `non-loopback URL shows privacy warning`
- `test/e2e/settings.spec.ts`:
  - `Test connection shows result inline` (via the mock)

### Verification

```bash
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/agentstate/ ./internal/laya/ ./internal/registry/ ./internal/session/ ./internal/agent/ ./internal/wire/
scripts/test.sh go unit dom e2e
for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./... || echo FAIL $os; done
(cd cmd/hivegui/frontend && npx biome ci . >/dev/null && npm run typecheck >/dev/null && echo OK)
scripts/check-daemon-contract.sh origin/main HEAD
# Real Laya (manual/local; CI has no server):
pip install "laya[serve]" && LAYA_PORT=8000 laya-serve &
HIVE_LAYA_URL=http://127.0.0.1:8000 go test ./internal/laya/ -run TestCorpusAccuracy -v
# UI: Playwright against the Wails mock checks the tooltip text and the Settings section (elementFromPoint), per the CSS/UI memory.
```

Each Go test above fails on a wrong implementation:
- If `Classify` refreshes `hookSeenAt`, `TestClassifyDoesNotRefreshHookClock` fails.
- If the call is made under the lock, `TestClassifierNotHeldUnderLock` deadlocks until its timeout and fails.
- If the tail is not taken, `TestClassifyRequestShape` asserts on the last line of a 200-line screen and fails.

## Open questions / risks

1. **Accuracy bar vs base checkpoint (needs operator decision).** Laya's base model is near chance zero-shot, and terminal text is untested. `TestCorpusAccuracy` may well score below 90%. You chose "build now, fine-tune later", so how should the gate treat that criterion?
   - **(a) Recommended:** ship with the feature default-off and the eval harness plus corpus in place. The gate checks that the harness runs and reports a number. The ≥90%/95% bar becomes a follow-up spec: "fine-tune a Laya checkpoint for terminal states".
   - **(b)** Keep the bar as a hard gate criterion, which risks a FAIL/NEEDS_FOLLOWUP.
1b. **Does "hooks firing" include heartbeats (needs operator confirmation)?** The plan says no. A Pi that only heartbeats and pings for 30 s, with no state event, is classifiable. That is what makes the "Pi stuck" case fixable. It also means a correct Pi "working" during a silent tool run longer than 30 s can be overridden by a Laya misread (4b).
2. **Corpus needs the operator.** Capturing real aider/codex/shell/pi screens in five states each requires running those agents. Implementation will stop at that step and ask you to run sessions with `HIVE_LAYA_CAPTURE_DIR` set; I cannot fabricate captures. Aider or codex may not be installed.
3. **LLM secret review sees the secrets.** The LLM pass reads the raw captures before redaction is confirmed. It runs locally in your own agent session, and the regex pass runs first to minimise exposure.
4. **Throughput.** Classification is sequential across sessions. If 10 sessions all go quiet at once, the 10th waits about 5 s, which breaks the 3 s bar for that session. The mitigation is at most one pending classification per session, newest first. A concurrency of 2 is a possible later tweak, since the server allows 16 concurrent requests but runs one worker.
4b. **Pi long tool runs.** A Pi turn running a silent 5-minute tool sends no events, so after 30 s it becomes classifiable. If Laya misreads that screen as idle or waiting, you get a false notification. This is accepted under "missed waiting is worse", and the corpus should include long-tool Pi screens.
4c. **Transient-error cost.** A screen whose classification errored is not retried until it changes.
5. **False waits stick.** A Laya waiting_input stays until ClearWaiting or new agent activity, the same as a bell today, so a false positive costs one notification plus a look. This is accepted given "missed waiting is worse".
6. **Non-loopback URL.** It is allowed, with a UI warning. An optional `LAYA_API_KEY` Bearer token is not supported (YAGNI for localhost); add it when someone runs a remote server.
7. Out of scope per the non-goals: Hive installing or running Laya, MCP, triage ranking, and the Pi hook root-cause.

## Second opinion

**Round 1: revise, confidence 8.** The reviewer raised 7 must-fix items, and all 7 were applied:
1. Pi's heartbeat refreshes `hookSeenAt` (`machine.go:388`), so a live Pi never goes stale. Fixed with a new `lastEventAt` clock.
2. Laya "working" could pin forever, and a reply landing after Tick could flicker. Fixed with `LayaRecheckAfter` plus a snapshot re-check before applying.
3. Error handling had no backoff and could hammer a dead server. Fixed with endpoint backoff and a recorded digest.
4. `Close` did not cancel or wait for the new goroutine. Fixed with its own cancel and done channels.
5. The registry tests were wall-clock flaky. Fixed with a deterministic `classifyCycle`.
6. The source-validation test was in the wrong package. Moved to the daemon.
7. Spec criteria 1 and 2 had no check CI can run. Added a deterministic 3 s test; the accuracy bar is left to the operator (open question 1).

**Round 2: revise, confidence 7.** The reviewer confirmed the round-1 fixes against the code and raised 3 new must-fix items. All 3 were applied after round 2, per the one-retry rule; there was no third review round.
1. The recheck could run away, because it was keyed on when a label was applied. It now uses `layaCheckedAt`, stamped on every attempt, and is covered by a test.
2. A Pi flicker loop: Output demoted the state, then a Replay restored the stale extension state. Fixed by leaving `Output` unchanged, so a trusted-by-heartbeat laya session only stamps `lastOutputAt`, plus a test.
3. The backoff test would have passed with no backoff at all. It now changes the screen every cycle and asserts calls land only at the backoff edges.

Also applied from the nice-to-haves:
- the `Close` nil-guard
- ping does not advance `lastEventAt`
- the source-constant parity test
- a 0600 capture file inside a 0700 directory
- the NNN changeset name
- `StaleAt` returns false for laya

Deferred: making `trusted()` return false for laya. It conflicts with fix 2 above.

## Decision log

- **2026-09-25** — Accuracy bar (spec criterion 1): ship default-off with corpus + eval harness; the gate checks the harness runs and reports a number; the ≥90%/95% bar moves to a follow-up spec (fine-tune a Laya checkpoint). Why: base checkpoint is near chance zero-shot; operator chose "build now, fine-tune later".
- **2026-09-25** — Pi heartbeats (Replay) and KindPing do not count as "hooks firing"; staleness is measured on `lastEventAt`. Why: operator decision; otherwise a heartbeating Pi is never classifiable.
- **2026-09-25** — Laya waits/errors raise attention and notifications. Why: operator — missed waiting is worse than a false one.
- **2026-09-25** — Corpus: capture via `HIVE_LAYA_CAPTURE_DIR`, regex scrub, then LLM review before commit. Why: operator choice; captures can contain secrets.
- **2026-09-26** — First corpus: 32 real screens (Codex 11, Pi 8, shell 13), captured from an isolated hived and labelled by construction. Scrubbed, with one username hand-redacted, and reviewed. Aider is dropped from the required agents because it isn't installed (operator decision; spec criterion amended). The Pi screens have no finished or permission turns, because Pi could not reach its oMLX model. Shell failures are labelled `idle`, following Hive's state model. Base English checkpoint on it: 37.5% overall, 87.5% waiting recall; working screens are the main false-wait source (5/14). This confirms accuracy as the follow-up.
- **2026-09-26** — Laya-produced waits are not sticky: `Output` clears them, and Laya may revise its own wait. Agent and bell waits are unchanged. Why: in the end-to-end run against a real laya-serve, a bash startup banner was classified `waiting_input`. Sticky like an agent's wait, it raised attention until looked at and hid the permission prompt printed next. Operator chose this over muting Laya notifications. After the change the same run classified the prompt `waiting_permission` 2.1 s after it was printed (`cmd/hived/laya_e2e_test.go`).
- **2026-09-26** — End-to-end against real laya-serve (English, MPS): warm call about 25 ms. 4–5 of 8 hand-picked screens were right across the three checkpoints, which is consistent with the near-chance zero-shot warning. Accuracy stays a follow-up.
- **2026-09-26** — Transport is Laya's `/v1/systemone`, superseding "oMLX first". Why: checked the operator's oMLX 0.6.4 — `/v1/models` lists 9 generative/embedding models and no Laya; MCP servers and tools are empty. The `aac6fef/laya-mlx` checkpoint sits in `~/.omlx/models` but is a custom decision-encoder format oMLX neither lists nor loads, and the `laya-mlx` package is a Python library with no HTTP server. Every Laya server (laya-serve, laya-server, Rapid-MLX + laya-mlx) speaks `/v1/systemone`, so the client targets it.
- **2026-09-26** — Capture writes every attempt, failed ones as `unclassified`. Why: the corpus is labelled by hand anyway, and it lets the operator capture screens before any Laya server is running.
- **2026-09-26** — A disabled classifier returns `registry.ErrClassifierOff`, which is not a failure (no stamp, capture or backoff). Why: otherwise switching Laya on would leave every already-seen screen unclassified until it changed.
- **2026-09-25** — First integration targets the operator's local oMLX (0.6.4, :8000); native `/v1/systemone` becomes a follow-up. Why: operator runs Laya via oMLX. oMLX exposes no systemone route; how Laya is reached through it (a model id vs. the oMLX MCP proxy) is pending operator verification, so the transport stays isolated behind the registry's `classify` func.
- **2026-09-25** — API key comes from the daemon env `HIVE_LAYA_API_KEY`, never agent-settings.json or the GUI. Why: operator choice; keeps the secret out of a plaintext file.
- **2026-09-25** — Spec gated via /hs-brainstorm: Laya is opt-in against a user-run server; hooks stay authoritative unless stale; missed "waiting" is the worse error. Why: operator answers.

## Progress

- **2026-09-25** — Research complete.
- **2026-09-25** — Plan approved (chat fallback after the HTML page timed out).
- **2026-09-26** — PR #464 opened.
- **2026-09-26** — Implementation complete except the corpus (needs operator captures). Go, unit, DOM and one e2e test green; `TestCorpusCoverage` red by design until captures land.

## Open questions

_None blocking — see the plan's Open questions / risks for accepted risks._

## PR convergence ledger

- **2026-09-26 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 12ba512e2cdcde9f7b214e5fff166ea4c15d93f31707eb9d4e1ab488809bf6fb; threads_open: 8; action: escalated:risky-fix-needs-decision; head_sha: d31cd770.
- **2026-09-26 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 3c9509c3ff6d830e2e4fb58fab063827e09392070404d69678e9f4c1f128cb84; threads_open: 1; action: escalated:risky-fix-needs-decision; head_sha: 3b8baf87.
- **2026-09-26 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 2b203193.
