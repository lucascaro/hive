# Dictate into a session from anywhere via a global capture HUD

- **Spec:** [docs/product-specs/553-dictate-into-a-session-from-anywhere-via-a-global.md](../../product-specs/553-dictate-into-a-session-from-anywhere-via-a-global.md)
- **Issue:** #553
- **Status:** active
- **PR:** #556
- **Branch:** feature/553-dictate-hud

## Summary

A Hive-owned global hotkey opens a floating, focus-taking capture HUD so any dictation tool (or typing) can fill it, then the user confirms which session receives the text. This plan covers how the hotkey, the HUD window and the confirmed write are built on macOS without adding any new programmatic text-injection path.

## Research

### Relevant code

- **SEND_TO_SESSION already exists** — frame 0x46 / reply SENT 0x47 (`internal/wire/frame.go:265-272`). Daemon handler `internal/daemon/daemon.go:2012-2055` (refuses plugin principals); registry `internal/registry/deliver.go:111` sanitises (strips ESC + C0, max 64 KiB, `:87-106`), tries the Claude/Pi inbox (`:182`), ACP via PromptACP, returns *busy* (or queues when `queue=true`) when the session is unsettled or has an unplaced `pending_prompt`. Callers: GUI ⇧⌘M message sheet (`cmd/hivegui/app_calls.go:977-986`, `frontend/src/app/modals/message-session.ts:79`), `hived msg` (`cmd/hived/msg.go:142`), ws-bridge, testclient (`internal/wire/testclient/client.go:392`). The HUD can reuse it: **no new text-carrying frame, socket or CLI is needed**.
- **Submit semantics** — `ptyBytes` maps `\n`→`\r` and appends `\r` (`deliver.go:234-236`); multi-line text becomes several submits on CR-submit TUIs (existing `ponytail:` note `:231-233`). Bracketed paste exists only in `internal/registry/create.go:891`.
- **hivebar** (`cmd/hivebar/`) — cgo via `fyne.io/systray v1.12.2` (`go.mod:6`, `build.sh:138-147`), `systray.Run` on main thread (`main.go:118-130`). Singleton flock `<state>/hivebar.lock` (`singleton.go:51-79`); spawned by both `hived` and `hivegui` through `internal/menubar` (`menubar_darwin.go:37-70`). One `ModeControl` conn (`client.go:107-111`) already keeps live `[]wire.SessionInfo`; `SessionRow{ID,Name,Title,Waiting,Alive}` and "name — title" labels (`model.go:56-65,232-253`). `request()` is fire-and-forget and ignores SENT/ERROR (`client.go:168,249-260`). No NSWindow/NSPanel code. README rule: reads nothing from the state dir except its lock.
- **hivegui** — one process per window (`app_calls.go:682-693`); Wails v2.15.0 is single-window, so a second floating window can't live there. No hotkey code anywhere in the repo.
- **Last-active session** — not known outside each GUI window: `activeId` is in-memory frontend state (`frontend/src/app/state.ts:390`); `SET_CLIENT_UI` carries only `plugin_uis` (`control.go:1819`). Daemon/registry track no focus.
- **SessionInfo** (`internal/wire/control.go:209+`): `id`, `name`, `title` (OSC, memory-only), `alive`, `state`, `needs_attention`, `pending_prompt`, `project_id`, `agent`.
- **Keymap** — `DEFAULT_APP_BINDINGS` (`frontend/src/lib/bindings.ts:62`); `<StateDir>/keymap.json` stored by Go, meaning owned by the frontend (`cmd/hivegui/keymap_prefs.go:5-11,35`).
- **Native cgo precedents** — `internal/notify/notify_darwin.go`, `internal/activity/activity_darwin.go`, `cmd/hivegui/loginitem_darwin.go`. hivebar Info.plist: LSUIElement, no entitlements.

### External

- **Global hotkey:** Carbon `RegisterEventHotKey` via cgo needs no TCC permission. `golang.design/x/hotkey` moved to CGEventTap in Jun 2026 and now requires Accessibility — avoid. `NSEvent` global monitors also need Accessibility.
- **Focus-taking panel:** Spotlight pattern = `NSPanel` + `.nonactivatingPanel`, `canBecomeKeyWindow=YES`, `makeKeyAndOrderFront`. macOS 14+ cooperative activation makes `activate` unreliable for accessory apps (Apple DTS, developer.apple.com/forums/thread/836619). An accessory app with no Edit menu may ignore ⌘V — add an Edit menu / `performKeyEquivalent`. **Unverified:** whether Handy's synthesized ⌘V and macOS Dictation land in a non-activating key panel while another app stays frontmost → must be spiked first.
- **Handy** v0.10.1 (2026-11-14): `handy --toggle-transcription` / `--cancel` / `SIGUSR2` remote-control the running instance; toggle only, no state query; docs call the CLI Linux-focused. Pastes via clipboard + ⌘V, needs Accessibility. Sources: handy.computer/docs/cli, handy.computer/docs/paste-methods.

### Constraints / dependencies

- HUD must be native (hivebar, cgo AppKit); hivegui cannot host it (Wails v2, process-per-window → duplicate hotkey registrations).
- Learning the last-active session needs a new *focus-report* control frame from GUI to daemon (not a text path) → `buildinfo.DaemonContract` bump + lock-step clients.
- A native HUD cannot read the TS bindings; Enter/Esc inside overlays are the policy's hard-coded exceptions.
- e2e-real never starts hivebar → HUD panel itself needs a manual/agent smoke (precedent: plan 551).

### Spike results (2026-11-15, macOS 26.7.1, operator-run)

Throwaway cgo test app (`hudspike`): Carbon hotkey + `NSPanel` (`NSWindowStyleMaskNonactivatingPanel`, `canBecomeKeyWindow=YES`) + `NSTextField`, Edit menu installed.

- Carbon `RegisterEventHotKey(⌃⌥Space)` registers (status 0) and fires while Chrome is frontmost. **No TCC prompt.**
- Panel becomes key with focus in the field (`isKey=1`); typing works, with no lag.
- **macOS Dictation (the user's own shortcut) lands in the field, but only when the test runs as a `.app` bundle** (LSUIElement). Run as a bare binary from the terminal, it did not.
- `startDictation:` sent programmatically (`--auto-dictate`) **does not** start dictation. So one-press dictation is not available: v1 takes two presses (Hive hotkey, then the user's dictation key). That matches the operator's fallback answer.
- `activateIgnoringOtherApps:` before showing the panel made focus late (`isKey=0`) and the panel laggy for seconds. **Do not activate**; rely on the non-activating key panel.
- Not tested: Handy (not installed on the operator's machine). Its paste is a synthesized ⌘V to the key window; the Edit menu with Paste is installed for that, and it is listed as an operator-run smoke row.

### Prior lessons

- Each Hive window is its own hivegui process; Go-side emits reach one window. "All windows" needs a daemon broadcast (PR 437).
- Smoke hivebar in isolation with `open -n --env HIVE_STATE_DIR=<tmp> -a hivebar.app`; UI scripting needs Automation permission an agent lacks, so list unclickable rows as operator-run (PR 552).

### Conventions card

- Build: `./build.sh` · Tests: `scripts/test.sh` (layers go · unit · dom · e2e); `npm run test:e2e:real` separately.
- CI toolchain: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`; per-GOOS `staticcheck` + `go vet` for darwin/linux/windows.
- Wire change: snake_case tags, update hivegui + ws-bridge + testclient in lock-step; bump `DaemonContract` (`scripts/check-daemon-contract.sh`).
- TDD: test with every change; non-PTY children via `internal/proc`; changeset in `.changesets/`, `site/features.json` entry (`since: "Unreleased"`) for a minor feature; README Keybinds table for new default keys.
- Keybindings Policy: no hard-coded keys outside Enter/Esc in overlays; labels derived from bindings.

## Approach

Keep the HUD and hotkey in **hivebar** (singleton, already running whenever the daemon is, already cgo/AppKit via systray). Deliver text through the **existing SEND_TO_SESSION** frame, the same one the ⇧⌘M message sheet uses. Add two small wire pieces, neither of which carries text:

1. **Focus report.** `SET_FOCUSED_SESSION` (C→S, control, `{session_id}`). The GUI sends it when its active session changes, and when its window gains focus. The registry stamps `FocusedAt` (unix ms, in memory, not persisted) on that session and broadcasts the normal `SESSION_EVENT`; `SessionInfo` gains `focused_at,omitempty`. hivebar's default target = the live session with the newest `focused_at`, or else none (the user picks). Plugin principals are refused, as SEND_TO_SESSION refuses them (`daemon.go:2012-2022`), so a plugin cannot steer the default target. **Storm guard:** the registry ignores a report for the session that is already newest when its stamp is under 1 s old, which means no stamp and no event. FocusedAt is in-memory only and never goes through `persistEntryLocked` (`registry.go:2176`). A timestamp rather than a single "focused id" copes with several GUI processes for free: the newest report wins.
2. **Global hotkey pref.** `SET_GLOBAL_HOTKEY` (C→S, `{key_code, modifiers, enabled}`, Carbon virtual key code + Carbon modifier mask), persisted by the registry to `<state>/global-hotkey.json` (atomic temp+rename). `GLOBAL_HOTKEY` (S→C) carries the current value to every control client after HELLO and on change. hivebar re-registers on receipt, idempotently: unregister, then register, so a reconnect that re-delivers the value is harmless. SET_GLOBAL_HOTKEY **refuses plugin principals** and validates server-side: key_code < 128, and modifiers limited to the Carbon cmd/shift/option/control bits with at least one set. **Single source of truth:** the chord's user-facing value lives in `keymap.json` (GUI-owned, like every other binding). The registry copy is a derived cache for hivebar. The GUI reconciles it, sending SET_GLOBAL_HOTKEY when the converted keymap value differs from the GLOBAL_HOTKEY it received: at boot, after every keymap load or import, and on a Settings save. That way a hand edit or import cannot drift. **Old daemon:** hivebar compares the daemon's HELLO contract the way `cmd/hivebar/model.go:68` does. If GLOBAL_HOTKEY is unsupported, the menu shows `Dictation hotkey: restart the daemon to enable` instead of failing silently. hivebar keeps its rule that it reads nothing from the state dir. The chord grammar stays in TS: Settings converts the chord to a Carbon pair (`lib/carbon-keys.ts`), so no grammar is ported to Go.

One `buildinfo.DaemonContract` bump covers both.

**Why not hivegui:** Wails v2 is single-window, and every window is its own process, so each would register the same hotkey. **Why not golang.design/x/hotkey:** since Jun 2026 it uses CGEventTap and needs Accessibility; Carbon via cgo needs none (spike-verified).

### HUD behaviour (hivebar)

- The hotkey opens the panel, records the frontmost app, and makes the panel key without activating the app.
- The panel shows: a target line `name — title` with a state badge; a native `NSPopUpButton` listing live sessions as "name — title" (↑/↓ while the field has focus moves the target, which keeps the field focused); a multi-line `NSTextView`-backed field; and a hint row `[enter] send · [esc] cancel · [↑↓] target`.
- **Enter** sends `SEND_TO_SESSION{queue:false}` to the target. Shift+Enter is a newline.
  - `SENT` closes the panel and reactivates the previous app.
  - `session_busy` keeps the panel open and shows `busy — [enter] queue · [esc] cancel`. A second Enter resends with `queue:true`. ACP sessions can't queue, so they show `busy — try again when idle`.
  - `session_dead` / `no_such_session` show an inline error and keep the text.
- **Esc** closes and sends nothing.
- With no live sessions, the panel shows `No live sessions` and Enter is inert.
- A 64 KiB cap is enforced in the HUD; the registry enforces it too.
- **Multi-line:** keep SEND_TO_SESSION's semantics (`\n`→`\r`, the same as ⇧⌘M). This closes the spec's open question with no new delivery mode.

### Threading (hivebar)

fyne systray v1.12.2 runs `onReady` on a goroutine, not the main thread (`systray@v1.12.2/systray.go:118-123`). Therefore:

- **Every** AppKit and Carbon call (panel build/show/hide, field reads and writes, popup updates, Register/UnregisterEventHotKey) is wrapped in `dispatch_async(dispatch_get_main_queue(), …)` inside the cgo layer.
- Callbacks arriving on the main thread (Carbon hotkey handler, key commands, popup changes) only post an event to a buffered Go channel, which is drained by the controller goroutine. They never call `Send` or take a Go lock on the main thread.
- The socket write in `Send` happens on the controller goroutine.
- The pure-Go tests can't catch a violation here. It is covered by a code-review checklist item and the smoke rows.

### Live session updates while the HUD is open

hivebar re-publishes on every SESSION_EVENT (`client.go:156-160`). The controller merges updates. It keeps the typed text and any manually chosen target, and refreshes the popup list in place. If the chosen target dies or disappears while the panel is open, it shows `target exited — pick another` inline and keeps the text. Enter is then inert until a live target is picked. The popup lists **all** live sessions, not capped at the menu's `maxSessionRows`.

### Keys and the Keybindings Policy

- The global chord is a new command `dictate-hud` with default `Ctrl+Alt+Space` (mac only). It lives in a **separate `GLOBAL_BINDINGS` list** in `bindings.ts`, not in `DEFAULT_APP_BINDINGS`, so the in-app resolver, the every-shortcut sweep (`every-shortcut.spec.ts:186-196`) and `menu-default-accelerators.json` never see it. In-app dispatch is impossible anyway, because the OS hotkey consumes the chord before the webview. Settings → Shortcuts and the help overlay read both lists: it is rebindable there, under a "Global (system-wide)" group, and hidden off macOS. Overrides are stored in `keymap.json` under the same `mac` map. Implementation checks `keymap-import.test.ts:36`, `plugin-chords.test.ts:31` and `bindings.test.ts`, extending them to cover GLOBAL_BINDINGS where they enumerate commands. RESERVED_CHORDS is untouched.
- Settings warns when a chord collides with an app binding. A global chord swallows the key in every app, Hive included.
- The HUD's own keys are Enter/Esc (the policy's overlay exceptions), plus native list navigation (↑/↓) and Shift+Enter (newline in a text field). A native AppKit panel cannot read the TS bindings. **Spec amendment requested at the plan stop:** criterion 6 changes to "The HUD shows its confirm, cancel and switch-target keys; confirm/cancel are the policy's Enter/Esc overlay exception, and target switching uses native ↑/↓ list navigation." Pushing HUD chords over the wire for keys a native control owns is speculative. The same PR adds ↑/↓ and Shift+Enter inside the native capture HUD to AGENTS.md's Keybindings Policy "Hard-coded exceptions", so the exemption is written down rather than implied.

### Code shape

- `cmd/hivebar/hud.go` is a pure-Go controller and state machine: Closed → Editing(target) → Sending → Busy → … It takes a `Sender` interface (`Send(id, text string, queue bool)`) and a `Results` callback, and is tested with a fake.
- `cmd/hivebar/hud_darwin.go` is a thin cgo NSPanel view: build, show, hide, set target list, set status, read text. Callbacks go into the controller.
- `cmd/hivebar/hotkey_darwin.go` does Carbon register and unregister on the main queue.
- `client.go` routes SENT and ERROR(session id) replies to the HUD. The HUD allows one in-flight send, so correlation is by session id; no req_id is needed.

### Files to change

1. `internal/wire/frame.go` — `FrameSetFocusedSession` (0x57), `FrameSetGlobalHotkey` (0x58), `FrameGlobalHotkey` (0x59), with String() names. Add the two C→S frames to `ControlRequestFrames` (`:324`) and GLOBAL_HOTKEY to `ControlEventFrames` (`:346`); `internal/wire/plugins_test.go:82` enforces this.
2. `internal/wire/control.go` — `SetFocusedSessionReq{SessionID}`, `GlobalHotkey{KeyCode uint32, Modifiers uint32, Enabled bool}` with snake_case tags; `SessionInfo.FocusedAt int64 json:"focused_at,omitempty"`.
3. `internal/registry/` — `SetFocused(id)` (stamps FocusedAt, emits event); `GlobalHotkey()` and `SetGlobalHotkey(h)` persisted atomically to `global-hotkey.json`, with the default `{kVK_Space=49, controlKey|optionKey=0x1800, enabled:true}` when missing.
4. `internal/daemon/daemon.go` — dispatch both C→S frames (plugin principals refused for **both** SET_FOCUSED_SESSION and SET_GLOBAL_HOTKEY; SET_GLOBAL_HOTKEY validates key_code < 128 and Carbon modifier bits, at least one set, unless enabled:false); send GLOBAL_HOTKEY to each control client after HELLO; broadcast on change.
5. `internal/buildinfo/contract.go` — bump DaemonContract, with a history comment entry. `docs/design-docs/daemon-contract.md` gets its history row.
5a. `docs/plugins.md` — request/event table rows for the three frames, marked plugin-refused where applicable. `internal/plugin/doc_test.go:20` enforces this.
6. `cmd/hivegui/app_control.go` (or `app_calls.go`) — Wails bindings `ReportFocusedSession(id)` and `SetGlobalHotkey(keyCode, mods, enabled)`.
7. `cmd/hived-ws-bridge/main.go` and `internal/wire/testclient/client.go` — lock-step support for the new frames.
8. `cmd/hivegui/frontend/src/app/state.ts` (or the active-session setter) — report focus on active change and on window focus, debounced.
9. `cmd/hivegui/frontend/src/lib/bindings.ts` — a new exported `GLOBAL_BINDINGS` list holding `dictate-hud` (mac `Ctrl+Alt+Space`, other: none), kept out of `DEFAULT_APP_BINDINGS` and needing no dispatcher flag. `src/app/commands.ts` gets the `dictate-hud` id with a title, and no `run` reachable in-app. `src/app/bindings.ts` `shortcutLabel` / `labelIn` resolve ids from both lists under the user's keymap. Keymap validation and import (`lib/keymap*.ts`) accept GLOBAL_BINDINGS ids.
10. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — a "Global (system-wide)" group row for `dictate-hud`, mac only.
11. Settings → Shortcuts (`src/app/settings/shortcuts*.ts`) and a new `src/app/global-hotkey.ts` reconciler: convert the effective `dictate-hud` chord via `carbon-keys.ts`, where an unbound or cleared chord becomes `{enabled:false}` and never an invalid code. Compare it with the last received GLOBAL_HOTKEY and call `SetGlobalHotkey` only when they differ, which cannot loop. Runs at boot, after every keymap load or import, and on Settings save. Settings warns on a global/app chord conflict. Mac only.
12. `cmd/hivebar/client.go` — handle GLOBAL_HOTKEY, SENT and ERROR for the HUD; add `SendToSession`.
13. `cmd/hivebar/main.go` — wire the hotkey and HUD after systray onReady.
14. `cmd/hivebar/model.go` — the `FocusedAt` field on SessionRow; default-target selection; a `hudContract` constant (the new DaemonContract generation) compared against `Welcome.DaemonContract` (`internal/wire/control.go:196`), the same way `stateContract` is. `cmd/hivebar/menu.go` gets a disabled `Dictation hotkey: restart the daemon to enable` item when the daemon is older, and `Dictation hotkey unavailable (in use)` on `eventHotKeyExistsErr`.
15. `cmd/hivebar/README.md` — "What it is allowed to do": it now sends user-confirmed text through SEND_TO_SESSION, and nothing else.
16. `DESIGN.md` — hivebar's role (HUD) and the new frames.
17. `README.md` — Keybinds table row (global dictate HUD ⌃⌥Space, macOS).
18. `.changesets/553-dictate-hud.md` (added, minor) and `site/features.json` (shipped, `since: "Unreleased"`).
20. `AGENTS.md` — Keybindings Policy hard-coded exceptions: the native capture HUD's ↑/↓ target navigation and Shift+Enter newline.
19. `cmd/hivebar/build/darwin/Info.plist` — no change needed (LSUIElement is already set; no mic usage).

### New files

- `cmd/hivebar/hud.go` — controller and state machine (all platforms, pure Go).
- `cmd/hivebar/hud_darwin.go` — cgo NSPanel view.
- `cmd/hivebar/hud_other.go` — no-op stub.
- `cmd/hivebar/hotkey_darwin.go` and `hotkey_other.go` — Carbon register/unregister.
- `cmd/hivegui/frontend/src/lib/carbon-keys.ts` — chord → Carbon (keyCode, modifiers).

### Tests

**Go:**
- `internal/registry`:
  - `TestSetFocusedStampsAndEmits` — FocusedAt increases; a SESSION_EVENT is emitted.
  - `TestSetFocusedUnknownSession` — an error, no stamp.
  - `TestGlobalHotkeyDefaultWhenMissing`.
  - `TestGlobalHotkeyPersistsAtomically` — the round-trip survives a reload; a corrupt file is an error and is not overwritten.
- `internal/daemon`:
  - `TestSetFocusedSessionRefusesPlugin`.
  - `TestGlobalHotkeySentAfterHello`.
  - `TestSetGlobalHotkeyBroadcasts` — a second control client receives it.
  - `TestSetGlobalHotkeyRefusesPlugin`, `TestSetGlobalHotkeyRejectsInvalid` (key_code ≥ 128, no modifier, unknown bits).
  - `TestSetFocusedFirstReportAfterRestartEmits`.
  - `TestSetFocusedRepeatNoEvent` — a repeat report for the already-newest session within 1 s emits no SESSION_EVENT.
  - `internal/wire`: `TestNewHUDFramesCarryNoText` — reflects over `SetFocusedSessionReq` and `GlobalHotkey` and fails if either gains a string field other than `session_id`. That pins criterion 9 for the new frames. Criterion 9 overall (no other new entry point) is checked at review/gate by diffing `ControlRequestFrames`.
- `cmd/hivebar/hud_test.go`:
  - `TestHUDDefaultsToNewestFocused`, `TestHUDDefaultSkipsDeadSessions`, `TestHUDNoSessionsEnterInert`.
  - `TestHUDEscSendsNothing`, `TestHUDEnterSendsOnceToTarget`, `TestHUDSwitchTargetSendsToNew`.
  - `TestHUDBusyThenEnterQueues`, `TestHUDBusyACPCannotQueue`, `TestHUDDeadTargetKeepsText`, `TestHUDOversizeRefused`.
  - `TestHUDSecondEnterWhileSendingIgnored` — no double send.
  - `TestHUDEscWhileSendingIgnoresLateSent` — a SENT after Esc neither reopens the panel nor reactivates the previous app.
  - `TestHUDKeepsTextAndManualTargetAcrossEvents`, `TestHUDTargetKilledWhileOpen`, `TestHUDListsAllLiveSessions` (more than `maxSessionRows`).
- `cmd/hivebar/client_test.go` — `TestClientRoutesSentAndErrorToHUD`, `TestClientAppliesGlobalHotkey`, `TestClientReappliesHotkeyOnReconnect` (register is idempotent), `TestClientOldDaemonShowsRestartHint`.

**JS:**
- `test/unit/carbon-keys.test.ts` — Ctrl+Alt+Space → (49, 0x1800); Mod is cmdKey on mac; an unknown key is rejected.
- `test/unit/bindings` — `dictate-hud` exists, is mac-only and is not dispatched in-app.
- `test/unit/keymap-import.test.ts` — `TestKeymapImportAcceptsGlobalCommand`: a `dictate-hud` override survives import.
- `test/unit/global-hotkey.test.ts` — reconcile sends when the keymap differs from the received value; sends nothing when they are equal (no loop); a cleared chord sends `{enabled:false}`.
- `test/dom` — Settings warns on a global/app conflict; saving calls `SetGlobalHotkey`; boot reconciles (a keymap value differing from the received GLOBAL_HOTKEY triggers `SetGlobalHotkey`; an equal one does not).
- every-shortcut sweep: unaffected, because `dictate-hud` is not in `DEFAULT_APP_BINDINGS`. `bindings.test.ts` asserts that GLOBAL_BINDINGS and DEFAULT_APP_BINDINGS share no command id or chord.

**e2e-real** (`frontend/test/e2e-real/`):
- `focus-report.spec.ts` — the GUI switching session → `SessionInfo.focused_at` updates.
- `global-hotkey.spec.ts` — Settings save → the daemon persists and broadcasts.

## Verification

- **What automated tests cannot prove:** criteria 1 (focus from an unfocused state plus dictation), 4 (nothing written on Esc, end to end) and 8 (the same flow when Hive is focused) rest on the operator smoke rows below. The controller tests prove only the state machine.
- `scripts/test.sh` (go · unit · dom · e2e) green; `npm run test:e2e:real` green.
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./...`; for each `os` in `darwin linux windows`: `GOOS=$os go vet ./...` and `GOOS=$os staticcheck ./...` (this covers the `cmd/hivebar` `*_other.go` stubs on linux and windows).
- `scripts/check-daemon-contract.sh origin/main HEAD` passes (the bump is present).
- `./build.sh`, then the operator-run smoke (an agent cannot press global keys). Rows:
  1. Chrome frontmost, ⌃⌥Space → the HUD has focus with the last-focused session shown.
  2. macOS dictation lands in the field.
  3. ↑/↓ switches target.
  4. Enter → the text arrives in that session only; focus returns to Chrome.
  5. Esc → nothing is written: `grep -c 'daemon: send_to_session' <hived log>` is unchanged before and after.
  6. A busy agent → the busy line, then Enter queues.
  7. No sessions → the empty state.
  8. Hive focused → the same flow.
  9. Rebind in Settings → the new chord works and the old one doesn't.
  10. Handy paste, if installed.

## Second opinion

- **Round 1** — independent reviewer (hs-reviewer): **revise**, confidence 7, 10 must-fix (wire frame lists, docs/plugins.md, contract docs, plugin refusal on SET_GLOBAL_HOTKEY, main-thread rules, keymap/registry drift, every-shortcut handling, focus-event storms, HUD state across live updates, vacuous TestNoNewTextPath, old-daemon behaviour). All 10 applied.
- **Round 2** — **revise**, confidence 8, 4 must-fix (Files list contradicted the revised Approach). All 4 applied, plus the nice-to-haves (Esc during send, first report after restart). No third round, per the loop.


## Decision log

- **2026-11-15** — Default target comes from a new GUI→daemon focus report (no text payload); hivebar reads it from session data. Why: operator choice; the heuristic would often pick the wrong session.
- **2026-11-15** — The global hotkey is rebindable in Settings → Shortcuts in v1. Why: operator choice, consistent with the Keybindings Policy.
- **2026-11-15** — Confirm on a busy session shows "busy" and offers a queue key (existing `queue` flag). Why: operator choice; never drop or silently delay text.
- **2026-11-15** — HUD lives in hivebar (native cgo AppKit NSPanel); hotkey via Carbon `RegisterEventHotKey`. Why: hivebar is a singleton that already runs when the GUI is unfocused or closed; Wails v2 can't host a second window and per-window GUI processes would register duplicate hotkeys; Carbon needs no Accessibility permission.
- **2026-11-15** — Text is delivered through the existing SEND_TO_SESSION frame. Why: satisfies "no new text entry point"; it already sanitises and handles inbox/ACP/busy.

- **2026-11-15** — v1 is two presses (Hive hotkey, then the user's dictation key). Why: spike showed `startDictation:` does not start dictation programmatically; operator accepted two presses if no better tool fits (Spokenly has start/stop URLs but adds a dependency).
- **2026-11-15** — Spec criterion 6 amended: HUD keys are the Enter/Esc overlay exception plus native ↑/↓ target navigation and Shift+Enter newline, written into AGENTS.md. Why: a native AppKit panel cannot read the TS bindings; approved at the plan stop.
- **2026-11-15** — Multi-line: keep SEND_TO_SESSION semantics (same as ⇧⌘M). Why: closes the spec's open question without a new delivery mode.
- **2026-11-16** — Deviation: `dictate-hud` lives in `DEFAULT_APP_BINDINGS` (`{mac:'Ctrl+Alt+[Space]', other:[]}`) with a declining command, not in a separate `GLOBAL_BINDINGS` list. Why: reuses the keymap, import, label and overlap machinery unchanged; the every-shortcut sweep lists it under DECLINES.
- **2026-11-16** — Deviation: an unreadable or invalid `global-hotkey.json` falls back to the default chord instead of failing `Open`. Why: the file is a cache of `keymap.json` that the GUI re-sends on every connect.

## Progress

- **2026-11-15** — Plan created; RESEARCH started.
- **2026-11-15** — Operator-run spike done (hotkey, key panel and dictation verified in a .app; programmatic `startDictation:` does not work).
- **2026-11-15** — Plan approved (HTML review, round 1); stage IMPLEMENT.
- **2026-11-16** — Implementation done; full `scripts/test.sh` green; operator smoke (isolated daemon) passed: ⌃⌥Space opened the HUD over Chrome, macOS dictation filled it, ↩ delivered one `send_to_session` to the focused session only, Esc sent nothing.

## Risks

- The Carbon hotkey API is deprecated-but-working (spike on 26.7.1). If Apple removes it, the fallback is CGEventTap plus an Accessibility prompt.
- Handy is untested; a synthesized ⌘V needs the Edit menu (installed).
- `focused_at` is in-memory, so after a daemon restart there is no default until the GUI reports one. Acceptable: the HUD then asks the user to pick.
- The chord could collide with a system shortcut (e.g. ⌃Space input-source switch). RegisterEventHotKey fails with `eventHotKeyExistsErr`, and hivebar shows "Hotkey unavailable" in its menu.
- Text in the HUD lives only in memory and is cleared on close.

## Open questions

None — criterion 6 amendment approved at the plan stop (2026-11-15).

## PR convergence ledger

- **2026-11-16 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: d8374aacda5b9a4629684f4ad6ecdc42620dd2df17cd6570d932bc66d4057676; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 13f70b2c.
- **2026-11-16 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 42cbaa3f8843378db5c06fc62db31a068af798adc58da35a43e99ebccaab200b; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 784672fa.
- **2026-11-16 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 39c19ba3.
