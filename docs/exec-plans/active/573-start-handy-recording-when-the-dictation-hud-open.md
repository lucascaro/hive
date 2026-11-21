# Start Handy recording when the dictation HUD opens

- **Spec:** [docs/product-specs/573-start-handy-recording-when-the-dictation-hud-open.md](../../product-specs/573-start-handy-recording-when-the-dictation-hud-open.md)
- **Issue:** #573
- **Status:** active
- **PR:** #574
- **Branch:** feature/573-handy-autostart

## Summary

This adds an opt-in setting that makes the menu bar's dictation HUD (spec 553) start Handy recording when it opens.
- Enter while still recording stops Handy and sends the transcription when it pastes in.
- Esc while recording cancels Handy.
- The setting travels in a generalised "dictation settings" payload that replaces the hotkey-only GLOBAL_HOTKEY frame.

## Research

### Relevant code

**Hotkey flow (to generalise).**
- GUI keymap reconcile: `cmd/hivegui/frontend/src/app/global-hotkey.ts:54-93`.
- GUI Go binding: `cmd/hivegui/app_calls.go:1046` `SetGlobalHotkey`.
- Wire:
  - frames `internal/wire/frame.go:323-324` (0x58/0x59), in the control lists at `:367,:382`;
  - type `internal/wire/control.go:1910` `GlobalHotkey` (with `Valid()` at `:1938`);
  - event name `internal/wire/client.go:160` `hotkey:global`.
- Daemon:
  - handler `internal/daemon/hud.go:43-60`;
  - plugin refusal `daemon.go:2544`;
  - hub `commands.go:46-51`;
  - sends after HELLO `daemon.go:1237` and on change `:1336`.
- Registry: `internal/registry/hud.go:62-112` (`global-hotkey.json`, default and corrupt-file fallback, setter returns `changed`).
- hivebar: `cmd/hivebar/client.go:211-215`, which feeds `Dictation.SetHotkey` (`hotkey.go:75`).

**HUD state machine.** `cmd/hivebar/hud.go`:
- phases `:82-89` (`hudEditing`/`hudSending`/`hudBusy`);
- `Open` `:185`, `Cancel` `:202`, `Submit` `:260`, `prepareSend` `:270-305`, `failed`/`OnSent`/`OnError` `:307-366`, `closeLocked` `:390`.

**Native view.** `cmd/hivebar/hud_darwin.m`:
- the Edit menu with Paste at `:98-116` is how a synthesized ⌘V reaches the field;
- Enter, Esc and ↑↓ go through `control:textView:doCommandBySelector:` at `:171-200`;
- there is **no text-changed callback**: text reaches Go only on Enter.

**Running commands from hivebar.**
- `cmd/hivebar/actions.go:60-102,220-240`: a direct `exec` behind an injectable `runner` with `runWithTimeout`.
- The darwin-only files are outside `TestNoDirectExecOnWindows`'s scope.

**Settings UI.**
- The Menu bar tab is gated by `MenuBarLoginItemStatus()`: `components/modals/Settings.tsx:138-143,311-322,654,1337-1365`, Go side `cmd/hivegui/loginitem_darwin.go:85` and `loginitem_other.go:13`.
- Stat precedent: `editor_path_darwin.go:21-23`.

### Handy probe (2026-11-20, operator-approved, Handy 0.10.1)

- `/Applications/Handy.app/Contents/MacOS/handy --toggle-transcription` **works on macOS**.
  - It exits 0 immediately.
  - The log shows "Microphone is receiving samples; recording is ready".
- `--cancel` stopped the recording within 2 s. The log shows "recording stopped after 2.1 s … Operation cancellation completed - returned to idle state". Nothing was pasted.
- The bundle id is `com.pais.handy`.
- `--help` documents both flags as "sent to running instance".

### Constraints / dependencies

- Handy can only toggle; it has no state query. The spec accepts this limitation.
- Handy delivers text through the clipboard plus a synthesized ⌘V (the default paste method). Direct Input mode types characters instead.
- A daemon-side change needs a `DaemonContract` bump (currently 39).
- The wire change must update, in lock-step:
  - `cmd/hivegui` and `cmd/hived-ws-bridge`;
  - `internal/wire/testclient`;
  - `wails-mock.ts` and `e2e-real/wails-bridge.ts`;
  - `docs/plugins.md`.

### Prior lessons

- Smoke-test a bundled helper with `open -n --env HIVE_STATE_DIR=<tmp> -a <bundle>`.
- Agents can't click menus (System Events needs Automation permission), so prove what can be proven headlessly and list the clicks as operator-run rows in the PR (brain entry from PR 552).

### Conventions card

- Build: `./build.sh`.
- Tests: `scripts/test.sh` (layers go · unit · dom · e2e).
- Go runs under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- Per-GOOS: `for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done`.
- Frontend: `npm run typecheck` (needs `./scripts/ci-bootstrap.sh`), `biome ci .`, Playwright with `CI=1`.
- Wire: `snake_case` JSON tags, with JS readers using `snake_case ?? camelCase`. Do not bump `PROTOCOL_VERSION`; bump `buildinfo.DaemonContract`.
- TDD: a test for every behaviour change, next to the source.
- Docs: add `.changesets/573-*.md` (`added`/`minor`), update README's dictation section and the `site/features.json` dictation entry; never edit `CHANGELOG.md`.

## Approach

### 1. The dictation-settings payload (replaces the hotkey-only frame)

**Frames.** A **new** pair is used: `FrameSetDictation` 0x63 and `FrameDictation` 0x64.
- 0x58/0x59 (`SET_GLOBAL_HOTKEY`/`GLOBAL_HOTKEY`) are retired: the daemon no longer handles or sends them, and the numbers are commented as reserved in `frame.go`.
- Why not reuse 0x58/0x59: hivebar is a separate login item and can outlive a GUI/daemon swap. An old hivebar decoding a new payload at 0x59 would read `enabled=false` and unregister the chord. With new numbers, an old hivebar just never receives a settings frame and keeps its last registration until it restarts (`TestOldHivebarIgnoresDictationFrame` pins this).

The new frames:

- `FrameSetDictation` (0x63, client→server, control) carries `wire.SetDictationReq{Hotkey *GlobalHotkey "hotkey,omitempty"; HandyAutoStart *bool "handy_auto_start,omitempty"}`.
  - A nil field leaves that setting alone.
  - Each GUI writer sends only its own field. The keymap reconcile sends `hotkey`; the Settings toggle sends `handy_auto_start`. Neither can clobber the other.
  - An empty request (both nil) is refused as invalid.
- `FrameDictation` (0x64, server→client) carries the full `wire.Dictation{Hotkey GlobalHotkey "hotkey"; HandyAutoStart bool "handy_auto_start"}`.
  - It is sent after HELLO and on change, exactly where GLOBAL_HOTKEY is today.
  - Its event name is `dictation:settings`, replacing `hotkey:global`.

**Contract.** The `DaemonContract` bump (39 to 40) makes a mismatched GUI restart the daemon instead of reloading into it. `PROTOCOL_VERSION` is unchanged, per DESIGN.md.

**Persistence.** The registry saves `dictation.json` in the state dir, written atomically.
- On first load with no `dictation.json`, it reads the legacy `global-hotkey.json` chord. A user's rebound chord survives the upgrade, and `handy_auto_start` starts false.
- The old file is left in place, read-only.
- A corrupt file falls back to the default, as today.
- `SetDictation(req)` merges the non-nil fields and reports `changed`.

**Daemon.** It validates the hotkey when present (`Valid()`), keeps the plugin refusal, and publishes to the existing hub, retyped to `hub[wire.Dictation]`.

### 2. GUI

**Go bindings.** `SetGlobalHotkey(key, mods, enabled)` is kept by name and now sends `SetDictationReq{Hotkey: &h}`. Two bindings are new:
- `SetHandyAutoStart(on bool)` sends `{HandyAutoStart: &on}`.
- `HandyInstalled() bool`, in `handy_darwin.go`, stats `/Applications/Handy.app` and `~/Applications/Handy.app` for `Contents/MacOS/handy`. A stub in `handy_other.go` returns false.

**`global-hotkey.ts`.**
- It listens on `dictation:settings` and reads `hotkey` plus `handy_auto_start` (snake ?? camel).
- Reconcile compares only the hotkey, so it is unchanged otherwise.
- It exports a tiny store, `useDictationSettings()` / `getDictationSettings()`, holding the last received `handy_auto_start`.

**Settings → Menu bar tab.** It gets a checkbox, "Start Handy recording when the dictation box opens".
- It renders only when `isMac && handyInstalled && received !== null`. A daemon with no settings yet shows nothing.
- It shows the daemon's value and writes through `SetHandyAutoStart`.
- Help text: "Enter while recording stops Handy and sends; Esc cancels it."

### 3. hivebar

**`handy.go`** (darwin) defines `type handy interface { Available() bool; Toggle() error; Cancel() error }`.
- `Available()` requires both of these:
  - the binary exists (same paths as the GUI);
  - Handy is running, checked through a native `NSRunningApplication` lookup for `com.pais.handy` added to `hud_darwin.m`.
- Hive never launches Handy, because `--toggle-transcription` on a stopped Handy would start it.
- `Toggle` and `Cancel` run the binary with `--toggle-transcription` / `--cancel` through the existing `runWithTimeout` runner (3 s), off the main thread.

**`client.go`.** It decodes `FrameDictation`, feeds `Hotkey` to `Dictation.SetHotkey` as today, and calls `hud.SetHandyAutoStart(b)`.

**`hud.go` state.** New fields: `autoStart bool`, `recording bool` (a Hive-started recording not yet known to have ended), `gen int` (bumped on every open and close), and phase `hudAwaitingText`. The `handy` and `afterFunc(d, f) (stop func())` dependencies are injected for tests.

**Threading rule.** Handy is never called under `h.mu` or on the AppKit main thread.
- Each method sets its state under the lock, captures `gen`, and enqueues the `Toggle`/`Cancel` on **one serial worker**: a goroutine draining a buffered channel, started in `NewHUD`. Calls run strictly in the order they were issued, so a close-cancel can never overtake the start-toggle it follows. Running each call in its own goroutine would let them race: an Esc right after open could cancel an idle Handy and then the start lands, leaving Handy recording with the HUD closed.
- The result comes back through a locked method, `handyResult(gen, op, err)`, which drops itself when `gen` has moved on, so a stale result can't change a closed or reopened HUD.
- `TestHUDHandyCallsNeverBlockTheLock` uses a fake whose `Toggle` blocks on a channel and asserts that `Open`, `Submit` and `Cancel` return first.

- **`Open()`**, on a fresh open only (not the refocus of an open HUD): if `autoStart && handy.Available()`, call `Toggle()`.
  - `recording = true` immediately, and the status line reads "Recording with Handy… [↩] stop & send".
  - If the async start fails, `handyResult` clears `recording` and the status reads "Couldn't start Handy".
- **`TextChanged(text string, pasted bool)`**, a new callback from the native field. Hive treats a **delivery** as the transcription arriving. A delivery is either of these:
  - a paste (`pasted`);
  - an insertion that adds 2 or more characters at once compared with the previous text. This also catches Handy's Ctrl+Shift+V method; typing one character at a time is never mistaken for it.
  - If `recording` and a delivery arrives: set `recording = false` (Handy stopped by its own key and delivered).
  - If phase is `hudAwaitingText` and the text differs from what it was at Enter: stop the timer, set `recording = false`, and send through `prepareSend`.
    - This branch accepts any change, not only a paste, so Handy's Direct Input mode, which types characters, still works.
    - It debounces 300 ms of quiet so a typed delivery isn't cut off mid-word, and each change re-arms the 10 s wait, capped at 60 s in total.
    - The send closes the HUD, and the close cancels Handy (below). That closes a race: if the user had already stopped Handy with its own key but the paste hadn't landed when they pressed Enter, Hive's stop-toggle *started* a new recording, and the cancel ends it.
- **`Submit(text)`**: if `recording`, start an async `Toggle()` to stop, then `phase = hudAwaitingText`, record the baseline text, and arm a 10 s timer.
  - The status reads "Waiting for Handy's transcription…".
  - On timeout: back to `hudEditing`, status "No transcription arrived from Handy. Nothing was sent.", text kept, `recording = false`.
  - If the stop `Toggle` fails (`handyResult`): back to `hudEditing`, `recording` stays true, timer stopped, status "Couldn't stop Handy. Stop it with its own key."
  - If not recording, Submit is unchanged, so after a transcription has arrived Enter never toggles Handy again.
- **Every close path resets.** `closeLocked` (reached from `Cancel`, `OnSent` and any other close) stops the timer and bumps `gen`. If Hive started Handy during this open (`startedHandy`), it starts an async `handy.Cancel()` and clears `recording`, `startedHandy` and the awaiting phase.
  - The cancel is unconditional on purpose. `--cancel` on an idle Handy is a no-op (probe: "returned to idle state"), so Hive doesn't need to know whether Handy is still listening. Nothing Hive started can outlive the HUD and paste into another app later.
  - `Disconnected()` while recording cancels Handy too: the HUD can't send, so a transcription would be stranded.
  - A paste arriving after the timeout lands in the field as plain text, because `recording` is already false; it is not sent.
  - A timer firing after close and reopen is dropped by its `gen`.
- **Enter while `hudAwaitingText`** is ignored, like Enter while sending.

**`hud_darwin.m`.** Implement `controlTextDidChange:` to call `goHUDTextChanged(text, pasted)`.
- `pasted` is true when `NSApp.currentEvent` is a keyDown with ⌘ and keyCode `kVK_ANSI_V`, which is Handy's synthesized ⌘V.
- An Edit-menu Paste also counts.
- Also export `hbHandyRunning()`.

**Menu bar status line.** When the setting is on but Handy isn't running, the dictation menu item reads "Dictate into a Session…  ⌃⌥Space (Handy not running)". The HUD still opens normally.

### 4. Why not the alternatives

The operator chose these trade-offs in the decision log:
- A bool on the hotkey frame has a misleading name and risks the reconcile clobbering the setting.
- A separate frame pair is double the plumbing.
- A state-file read breaks spec 553's rule.
- Using paste (not any text change) as "recording ended" while still recording means a user typing while Handy listens doesn't fool Hive into thinking Handy stopped.

## Files to change

1. `internal/wire/frame.go`: retire 0x58/0x59 (comment as reserved), add 0x63 `SET_DICTATION` / 0x64 `DICTATION`, and update the control and name lists.
2. `internal/wire/control.go`: add `Dictation` and `SetDictationReq`; `GlobalHotkey` stays as the nested chord type.
3. `internal/wire/client.go`: event name `dictation:settings`.
4. `internal/wire/testclient/client.go`: replace the helpers with `SetDictation` / `ReadDictation`.
5. `internal/registry/hud.go` + `registry.go`: `dictation.json` with legacy migration, `Dictation()`, `SetDictation(req) (changed bool, err)`.
6. `internal/daemon/hud.go`, `commands.go`, `daemon.go`: the handler, the retyped hub, the after-HELLO send (`daemon.go:1237,1336`), `pluginRefusedFrames` (`:2544`) listing the new constants, and the comment at `:101`.
7. `internal/buildinfo/contract.go`: bump `DaemonContract` to 40, with a history line.
8. `cmd/hived-ws-bridge/main.go`: the `SetGlobalHotkey` case sends the new request; add a `SetHandyAutoStart` case.
9. `cmd/hivegui/app_calls.go`: the `SetGlobalHotkey` body, plus the new `SetHandyAutoStart`.
10. `cmd/hivegui/frontend/src/app/global-hotkey.ts`: the new event, payload reader and settings store.
11. `cmd/hivegui/frontend/src/bridge.ts`: export the new bindings.
12. `cmd/hivegui/frontend/src/components/modals/Settings.tsx`: the Menu bar tab checkbox.
13. `cmd/hivegui/frontend/test/e2e/wails-mock.ts` and `test/e2e-real/wails-bridge.ts`: the mocks and real bridge for the new bindings and event.
14. `cmd/hivebar/client.go`, `hud.go`, `hud_native.go`, `hud_darwin.m/.h`, `hotkey.go` (menu label), `main.go` (wiring the Handy runner).
15. `plugins/sdk/hive-plugin.mjs`, `plugins/webhook/hive-plugin.mjs`, `plugins/workflows/hive-plugin.mjs`: replace the frame constants with `SET_DICTATION: 0x63` / `DICTATION: 0x64`.
16. `docs/plugins.md:315,333,380`: all three references. `DESIGN.md:77`: the frame list. `docs/design-docs/control-plane.md`, if it lists them. `cmd/hivegui/frontend/src/lib/carbon-keys.ts:162`: comment. `cmd/hivegui/frontend/src/main.tsx:88,170`: only if the init function is renamed (it isn't; `initGlobalHotkey` stays).
17. `README.md`:
    - the dictation section (`:258-278`): the opt-in, Enter stops and sends, Esc cancels, the already-recording limitation;
    - replace "a second press starts your dictation tool".
18. `AGENTS.md`: the HUD hard-coded keys note (Enter stops Handy and sends while recording).
19. `site/features.json`: extend the dictation entry's blurb.
20. `.changesets/573-handy-autostart.md`: `added`, `minor`.

## New files

- `cmd/hivebar/handy.go` (darwin): the `handy` interface, the exec-backed implementation and path lookup.
- `cmd/hivebar/handy_test.go`: lookup and runner args.
- `cmd/hivegui/handy_darwin.go` / `handy_other.go`: `HandyInstalled`.

## Tests

**Wire.** `internal/wire/hud_test.go`:
- update `TestHUDFramesCarryNoText` to walk `Dictation` and `SetDictationReq` recursively (pointers and nested structs) and still reject any string field, rather than weakening it;
- `TestRetiredHotkeyFramesAndNewDictationFrames`: 0x58/0x59 are absent from the control lists and 0x63/0x64 are present. Decoding a 0x64 body with the legacy flat `GlobalHotkey` is never attempted, because an old hivebar dispatches on frame number, so the number itself is the protection;
- `TestDictationRoundTrip`: snake_case keys and nil fields omitted;
- `TestSetDictationReqEmptyInvalid`.

**Registry.** `internal/registry/hud_test.go`:
- `TestDictationDefaultWhenMissing`;
- `TestDictationMigratesLegacyHotkeyFile`: an old `global-hotkey.json` chord is kept and `handy_auto_start` is false;
- `TestSetDictationMergesFields`: setting the bool leaves the chord alone, and the reverse;
- `TestDictationPersists`;
- `TestDictationCorruptFallsBack`;
- `TestDictationCorruptNewFileIgnoresLegacy`: a corrupt `dictation.json` next to a valid `global-hotkey.json` falls back to the default, not the legacy chord, because the new file is the truth once written;
- `TestDictationFirstSaveWritesNewFileOnly`: the save writes `dictation.json` and leaves `global-hotkey.json` byte-identical;
- the existing write-failure and invalid tests, renamed.

**Daemon.** `internal/daemon/hud_test.go`:
- `TestSetDictationRefusesPlugin`, `TestSetDictationRejectsInvalidHotkey` (these replace the existing tests at `:61-97` that use the old constants);
- `TestRetiredGlobalHotkeyFrameRefused`: a 0x58 frame now gets the unknown-frame error;
- `TestSetDictationBroadcastsFullSettings`: a bool-only set broadcasts the unchanged chord too.

**e2e.** `cmd/hived/hud_e2e_test.go` `TestE2E_DictationSentAfterHelloAndBroadcast`: the renamed test, also covering `handy_auto_start`.

**hivebar.** `cmd/hivebar/hud_test.go`, with fake Handy and fake timers:
- `TestHUDAutoStartTogglesHandyOnOpen`;
- `TestHUDAutoStartOffNeverTouchesHandy`;
- `TestHUDAutoStartSkippedWhenHandyUnavailable`: behaves exactly as today;
- `TestHUDRefocusDoesNotToggleAgain`;
- `TestHUDEnterWhileRecordingStopsAndSendsOnText`: one Toggle to start, one to stop, and the send carries the pasted text;
- `TestHUDEnterWhileRecordingTimesOutSendsNothing`: a status message, text kept, nothing sent;
- `TestHUDPasteEndsRecordingSoEnterDoesNotToggle`;
- `TestHUDTypingWhileRecordingDoesNotEndRecording`;
- `TestHUDEscWhileRecordingCancelsHandy`;
- `TestHUDEscWhileAwaitingCancelsHandy`;
- `TestHUDEnterWhileAwaitingIgnored`;
- `TestHUDDirectInputDeliveryDebounced`;
- `TestHUDHandyToggleErrorShowsStatus`: a failed start;
- `TestHUDStopToggleFailureReturnsToEditing`: `recording` stays true, the status is shown, and the timer is stopped;
- `TestHUDHandyCallsRunInOrder`: a blocking first call; Open then an immediate Esc, and Open, Enter, Esc, each run as Toggle, then Toggle or Cancel, in issue order;
- `TestHUDHandyCallsNeverBlockTheLock`: a blocking fake; `Open`, `Submit` and `Cancel` return before `Toggle` completes;
- `TestHUDStaleHandyResultIgnoredAfterReopen`: a result from a closed HUD's `gen` changes nothing;
- `TestHUDCloseAlwaysCancelsHandyItStarted`: Esc, send and disconnect each run `Cancel` exactly once when Hive started Handy, and never when it didn't (auto-start off, or Handy unavailable);
- `TestHUDDisconnectWhileRecordingCancelsHandy`;
- `TestHUDLatePasteAfterTimeoutIsNotSent`;
- `TestHUDTimerAfterCloseAndReopenIgnored`;
- `TestHUDMultiCharInsertionCountsAsDelivery`, `TestHUDSingleCharTypingIsNotDelivery`;
- `TestHUDWaitReArmsOnChangeUpToCap`.
- The fake-driven paste tests prove the HUD logic only. The real detection in `hud_darwin.m` (`controlTextDidChange:` and `NSApp.currentEvent`) has no automated coverage, so operator smoke row 3 is **mandatory before merge**.

**hivebar client and menu.**
- `cmd/hivebar/client_test.go` `TestClientDictationFrameFeedsHotkeyAndHUD`, replacing the old-constant test at `:176`;
- `hotkey_test.go` `TestMenuLabelHandyNotRunning`, plus `TestMenuLabelNoHandySuffixWhenSettingOff`;
- `handy_test.go` `TestHandyRunsToggleAndCancelFlags` (fake runner).

**Frontend.**
- `test/dom/global-hotkey.test.ts`: the event rename; reconcile still sends only the hotkey, and a bool-only broadcast causes no resend.
- `test/dom/settings.test.tsx`:
  - `shows the Handy toggle only when Handy is installed on mac`;
  - `toggle writes SetHandyAutoStart`;
  - `hidden with no daemon settings`.
- `test/e2e/settings.spec.ts`: the toggle visible with the mock reporting Handy installed, and a click round-trips through the mock.

## Verification

- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/wire/ ./internal/registry/ ./internal/daemon/ ./cmd/hivebar/ ./cmd/hivegui/`
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test -tags e2e -run 'Dictation' ./cmd/hived/`
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done`
- `CGO_ENABLED=1 go build -o /dev/null ./cmd/hivebar` on macOS: the only compile check of `hud_darwin.m`.
- `scripts/check-daemon-contract.sh origin/main HEAD`
- `cd cmd/hivegui/frontend && npm run typecheck && npx biome ci . && npx vitest run test/dom/global-hotkey.test.ts test/dom/settings.test.tsx && CI=1 npx playwright test test/e2e/settings.spec.ts`
- `git grep -nE "SET_GLOBAL_HOTKEY|GLOBAL_HOTKEY|FrameSetGlobalHotkey|FrameGlobalHotkey|hotkey:global" -- ':!CHANGELOG.md' ':!docs/exec-plans' ':!docs/product-specs'` must print only the reserved-number comment in `internal/wire/frame.go` and the contract-36 history entry in `internal/buildinfo/contract.go:58-59`. History entries are never rewritten.
- `git grep -n "global-hotkey.json" -- ':!CHANGELOG.md' ':!docs'` must print only `internal/registry/hud.go` (the migration) and its test.
- **Operator smoke** (headless can't drive Handy or the mic). Row 3 is mandatory before merge:
  1. Settings shows the toggle with Handy installed.
  2. One ⌃⌥Space starts Handy.
  3. Stopping with Handy's key pastes into the HUD and nothing is sent.
  4. Enter while recording sends.
  5. Speaking nothing, then Enter, shows "No transcription arrived".
  6. Esc while recording leaves Handy idle and pastes nothing elsewhere.
  7. Quitting Handy, then ⌃⌥Space, opens the HUD with no recording.

## Open questions / risks

- **Paste detection is unverified.** It assumes Handy's ⌘V arrives as a keyDown event with ⌘ and keyCode 9. The multi-character insertion rule is the fallback that holds either way, since Handy delivers whole phrases at once. The remaining miss is a one-word transcription of a single character. In that case a later Enter would start a recording, and the post-delivery `Cancel` doesn't apply because no Enter-stop happened. Accepted, and the status line shows "Recording with Handy…".
- **The user pastes their own clipboard while Handy is recording.** This counts as a delivery, so `recording` clears and Enter sends without the stop-toggle. Handy is still cancelled when the HUD closes, so it can't paste elsewhere later; the only cost is losing that dictation.
- **Already recording when the HUD opens.** The toggle stops it. This is accepted in the spec.
- **A 10 s wait may be short** for a very long dictation on a slow model. It is a named constant. Handy transcribed 2 s in about 160 ms on this machine.
- **A new hivebar paired with an old hived** (hivebar can start hived) never receives 0x64, so no chord is registered until the GUI restarts the daemon over the contract mismatch. The contract-40 history line will say so.
- **The 2-character insertion rule also fires** on IME composition or autocomplete while recording. Accepted: the only effect is skipping the stop-toggle, and the close-cancel still stops Handy.
- **Renaming a wire frame touches plugin SDK constants.** Plugins are refused both frames, so nothing behaves differently.

## Second opinion

- **Round 1** — an independent reviewer (hs-reviewer) returned **revise**, confidence 7, with 7 must-fix items, all applied:
  - Handy exec happening under the lock or on the main thread;
  - an undefined stop-failure path;
  - a race between Handy's own-key stop and the paste;
  - recording not reset on every close path;
  - reusing frames 0x58/0x59 would wipe the hotkey on a still-running old hivebar (new frames 0x63/0x64 now);
  - blast-radius gaps;
  - vacuous or incomplete verification.
- **Round 2** — **revise**, confidence 8, with 2 must-fix items, both applied:
  - Handy calls run on one FIFO worker, so a cancel can't overtake a start;
  - the grep gate now allowlists the contract-36 history entry.
- Disposition: per the loop rule, presented after round 2 with no third review.

## Decision log

- **2026-11-20** — The setting is opt-in, lives in GUI Settings, appears only when Handy is installed, and is off by default. Why: operator answer at the brainstorm.
- **2026-11-20** — The already-recording case is accepted: Handy has no state query. Why: operator answer.
- **2026-11-20** — Esc while recording runs `handy --cancel`. Enter while recording stops Handy, waits for the paste, and sends with no review. If no paste arrives, nothing is sent, the HUD says why, and the text is kept. Why: operator answers.
- **2026-11-20** — Plumbing: generalise GLOBAL_HOTKEY into a dictation-settings payload (chord plus `handy_auto_start`). Each GUI writer sets only its own field. Why: operator chose it over a bool on the hotkey frame (a misleading name and a reconcile-clobber risk), a separate frame (more plumbing), or a state file (breaks spec 553's rule that the menu bar reads nothing from the state dir).
- **2026-11-20** — Use Handy's CLI flags rather than `SIGUSR2`. Why: the probe proved both flags work on macOS, and `--cancel` has no signal equivalent.

- **2026-11-20** — Dropped the planned "(Handy not running)" suffix on the menu bar's dictation item. Why: hivebar has no menu-open hook, so the label would go stale when Handy starts or quits; the HUD itself falls back to spec 553 behaviour when Handy is not running.
- **2026-11-20** — A failed Handy start keeps `startedHandy` set, so the close still runs `--cancel`. Why: a stop-toggle queued behind the failed start may have started a recording, and `--cancel` on an idle Handy is a no-op.

## Progress

- **2026-11-20** — Research done; Handy CLI verified on macOS.
- **2026-11-20** — Plan approved (chat, after the HTML page timed out unanswered).
- **2026-11-20** — Implemented; PR #574 opened. Go, vitest (2281), Playwright (599), biome, typecheck, vet and staticcheck on three platforms all green locally.

## Open questions

- None blocking.

## PR convergence ledger

- **2026-11-20 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: a2abe1ad9df6ec658bf15aecd258ffe62ec5ec591678752c6dc814aa3e30079f; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: cc770ddf.
- **2026-11-21 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 318aff8119c3e5327ace6f2cd742a0709c940a3e97a8bdf6f2cec66e7d382c21; threads_open: 5; action: escalated:risky-fix-needs-human-decision; head_sha: 1e731747.
- **2026-11-21 iter 3** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 138cb05e18304cc3648e68c86d3c1d8b9cb7c09655f5e6ba9747f356acf2ed2b; threads_open: 2; action: escalated:risky-fix-needs-human-decision; head_sha: 9c10822e.
- **2026-11-21 iter 4** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 4a6425d6.

## Gate verdict
