# Bring the dictation box and Handy auto-start to Windows

- **Spec:** [docs/product-specs/575-bring-the-dictation-box-and-handy-auto-start-to-w.md](../../product-specs/575-bring-the-dictation-box-and-handy-auto-start-to-w.md)
- **Issue:** #575
- **Status:** active
- **PR:** #576
- **Branch:** feature/575-windows-dictation

## Summary

Port the spec 553 dictation box and the spec 573 Handy auto-start to Windows. The hotkey is live only while a Hive GUI window runs, and there is no tray agent. The full why lives in the spec; this file is about the how.

## Research

### Relevant code

**Mac box, the code to port** (all `cmd/hivebar`, all `//go:build darwin`):
- `hud.go` (890 lines). The box state machine. It is pure Go: stdlib, `internal/agent` and `internal/wire` only.
  - It drives a `hudView{Show(hudState, clear, focus); Hide(restore)}` (hud.go:57-68).
  - Native calls back into `Submit`, `Cancel`, `Move`, `Select`, `TextChanged(text, pasted)` and `Open` (hud.go:313-480).
  - The wire client feeds `SetSessions`, `Disconnected`, `OnSent`, `OnError`, `SetHandyAutoStart` and `Shutdown`.
  - It sends through `hudSender.SendToSession(id, text, queue)` (hud.go:72).
  - It depends on `SessionRow.LabelIn` (model.go:56/232), `handyCtl`, `errHandyNotRunning` and `handyTimeout` (handy.go).
- `handy.go`: the `handyCtl` interface is neutral. The data is darwin: the bundle id, `.app` paths, `nativeHandyRunning` (cgo), and `isExecutable`, which checks the exec bit and fails for `.exe`.
- `hotkey.go`: the `Dictation` controller and `registrar` interface (:31) are neutral, but `keyNames` and `hotkeyLabel` are macOS glyphs.
- `client.go`: neutral routing (`handleFrame` :169-231, `hudEvents` :79-94). It dials a unix socket, which the GUI doesn't need because it has its own control connection.
- Tests: `hud_test.go`, `hud_handy_test.go` and `hotkey_test.go` use fakes only. The one exception is the execHandy test (hud_handy_test.go:508-521), which uses `/bin/sh`.

**Wire and daemon.** Platform-neutral, no change expected.
- Sending: `SEND_TO_SESSION` 0x46 returns `SENT` 0x47 or `ERROR` (internal/daemon/daemon.go:2222-2267, internal/registry/deliver.go:111). The daemon sanitizes input and appends `\r`.
- Default target: the largest `FocusedAt`, stamped by `SET_FOCUSED_SESSION` 0x57 through `App.ReportFocusedSession` (cmd/hivegui/app_calls.go:1037).
- `wire.GlobalHotkey{KeyCode kVK, Modifiers Carbon, Enabled}` (internal/wire/control.go:1903-1943), carried in `DICTATION` 0x64 and set by `SET_DICTATION` 0x63.

**GUI host (Windows).**
- There is no single-instance or ownership mechanism between hivegui processes (cmd/hivegui/main.go:98-136, window_windows.go:15-40).
- Wails v2.15 offers one window per app and no WndProc hook. Its main loop drops thread messages (winc/app.go:76-90).
- Existing Win32 use through `golang.org/x/sys/windows`: clipboard_windows.go and pick_directory_windows.go.
- No cgo on Windows. hivegui.exe is built by `wails build -platform windows/amd64`.

**Frontend gates (all macOS-only today):**
- global-hotkey.ts:43 (`reportFocus`), :75 (`reconcile`) and :123 (`init`).
- bindings.ts:145: `dictate-hud` is `other: []`.
- shortcuts.ts:438-456: groups pushed only `if (isMac)`.
- Settings.tsx: :694 `showMenuBar = isMac && …`; the Handy switch at :1406-1430 lives inside the Menu bar panel, which is absent off mac.
- Chord to wire: lib/carbon-keys.ts (`KeyboardEvent.code` to kVK, Carbon bits; `parseChord(chord, true)`).
- `handy_other.go` returns false.

**Tests to adapt:**
- test/dom/global-hotkey.test.ts (mocks `isMac: true`)
- settings.test.tsx:1300-1323
- settings-shortcuts.test.tsx:177
- test/unit/shortcut-labels-parity.test.ts:36-71
- test/e2e/settings.spec.ts:434-450 and :619-644
- every-shortcut.spec.ts:115-122 (`dictate-hud` DECLINES)

**Docs saying macOS-only:**
- README.md:257-285, :426
- AGENTS.md:249-253
- DESIGN.md:73-81
- docs/plugins.md:314-315, :333, :379-380
- cmd/hivegui/handy_other.go:9

### Constraints / dependencies

- One hotkey owner across N hivegui processes. `RegisterHotKey` fails with 1409 if the chord is already taken, so it can act as its own lock, with losers retrying. It is thread-affine and needs our own `LockOSThread` goroutine with a `GetMessageW` loop.
- The box must be a native Win32 window (CreateWindowEx, an EDIT child, WS_EX_TOPMOST|TOOLWINDOW) built through syscalls on that same thread. A second Wails window is impossible in v2.
- Exec handy.exe only via `proc.Command`/`proc.CommandContext` (`TestNoDirectExecOnWindows`, internal/proc/edict_test.go:24-44).
- Handy on Windows:
  - its install path is undocumented (docs example: `%ProgramFiles%\Handy\handy.exe`);
  - its CLI is "beta" and unverified;
  - "running" needs a process probe (Toolhelp32 / `handy.exe`), because a CLI call on a stopped Handy launches it.
- CI's Windows leg runs `go build` and `go vet` only. Staticcheck runs on Linux, so run `GOOS=windows staticcheck` locally.
- The `other` binding also covers Linux, so a Linux default would expose a global chord that does nothing.

### Prior lessons

- `handy-cli-flags-work-on-macos`:
  - `--toggle-transcription` and `--cancel` forward to the running instance and exit 0 at once;
  - `--cancel` is idempotent;
  - on a stopped Handy either flag launches it, so check Handy is running first;
  - there is no state query.
- `ui-state-machine-driving-external-process-single-queue` (from 573): serialize all Handy calls on one worker queue, outside the lock.

### Conventions card

- Build: `./build.sh` (macOS); `./build.sh --platform windows` (hivegui.exe + hived.exe).
- Tests: `scripts/test.sh [go|unit|dom|e2e]`. Playwright with `CI=1`. Go under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- Lint:
  - `for os in darwin linux windows; do GOOS=$os staticcheck ./...; GOOS=$os go vet ./...; done`
  - `npx biome ci .`
  - `npm run typecheck`
  - `scripts/ui-lint.sh`
- TDD: tests with every change. Keybinding changes follow the Keybindings Policy:
  - bindings.ts
  - shortcuts.ts rows
  - README Keybinds table
  - a changeset
- `site/features.json` entry for a user-visible feature. DaemonContract bump only if wire/daemon change. Windows rows are verified on the `windows-vm` MCP (docs/testing-on-windows.md).

## Approach

Three layers. Only the bottom one is new native code.

1. **Shared, cgo-free dictation core: new `internal/dictation`.**
   - Move the box state machine out of `cmd/hivebar/hud.go` unchanged in behaviour.
   - Move the hotkey controller out of `cmd/hivebar/hotkey.go` (minus its macOS key names and glyph label, which stay in hivebar).
   - Move the neutral half of `cmd/hivebar/handy.go`.
   - hivebar becomes a host:
     - its AppKit panel implements `dictation.View`;
     - its Carbon code implements `dictation.Registrar`;
     - its wire client feeds the HUD as today.
   - The move is mechanical:
     - types become exported where hivebar needs them (`HUD`, `View`, `State`, `Target`, `Sender`, `HandyCtl`, `Hotkey`/`Registrar`);
     - `SessionRow.LabelIn` becomes `dictation.Label(project, name, title)`, which hivebar's model.go calls too;
     - the macOS key glyphs in hints and status texts come from an injected `dictation.KeyNames{Enter, NewLine}`: `↩`/`⇧↩` on macOS, `Enter`/`Shift+Enter` on Windows.
   - Why extract rather than copy: the 573 state machine is the riskiest code in the feature, and it took four review rounds to settle. One copy keeps both platforms on the same tested logic. Its tests then run on Linux and Windows CI too. Today they run only on macOS.
   - Handy execution uses `proc.CommandContext` and keeps all of `runWithTimeout`'s semantics (actions.go:223-240): the 3 s deadline, `cmd.WaitDelay = 500ms` so a grandchild holding the pipes can't stall the Handy worker, stderr surfaced in the error, and DeadlineExceeded wrapping. It replaces hivebar's direct `exec.CommandContext`. This is required by `TestNoDirectExecOnWindows`, and it is harmless on macOS.
   - Platform data sits behind build tags:
     - `handy_darwin.go`: the bundle paths, plus a "running" probe the host injects (hivebar passes its cgo `NSRunningApplication` check).
     - `handy_windows.go`:
       - the candidate paths are `%LOCALAPPDATA%\Programs\Handy\handy.exe`, `%LOCALAPPDATA%\Handy\handy.exe` and `%ProgramFiles%\Handy\handy.exe`, built from an injected getenv so they test anywhere (the real path is confirmed in the VM and the list trimmed to match);
       - the "running" check is a Toolhelp32 snapshot looking for `handy.exe`;
       - "executable" is a regular file, with no exec-bit check.
   - `dictation.HandyBin()` replaces the duplicate path list in `cmd/hivegui/handy_darwin.go`. `handy_other.go` becomes `!darwin && !windows`.
   - **Connection.** `dictation.Conn`:
     - Takes an injected dial func, so tests use `net.Pipe`.
     - The GUI supplies a dial-only func, `CheckSocketDir` plus `net.DialTimeout("unix", a.socketPath())` plus `wire.Handshake` with a `ModeControl` HELLO and client `hive-dictation/<ver>`. It is never `dialHandshake`, which goes through `dialOrSpawn` (app_control.go:29-41, app_spawn.go:56-76). Using that would respawn a daemon the user just quit, and could consume the process's single `spawnedHived` CompareAndSwap before the boot `ConnectControl` does.
     - Routes SESSIONS, PROJECTS, SESSION_EVENT, PROJECT_EVENT (re-list), SENT, ERROR (with a session id) and DICTATION into the HUD and hotkey controller.
     - Redials on loss, and never spawns hived.
     - It is a dedicated connection rather than a tee of the GUI's control loop, because that loop forwards every frame to the frontend. A SENT or ERROR answering the box's send would then reach the message sheet, and the sheet's own sends would reach the box.
   - **The daemon must not treat this conn as a UI client.**
     - `canAnswer` (internal/daemon/planreview.go:29) today excludes only `hivebar/`. A `hive-dictation/` conn would be counted as an answerer (daemon.go:800), subscribed to every PLUGIN_MESSAGE (daemon.go:1173) and given `announceUI` (daemon.go:1374).
     - While a window's real GUI conn is down (reload or reconnect), a plan review or worktree park would then wait on a client that cannot answer (#451/#457).
     - Fix: `canAnswer` excludes a named set of headless clients, `hivebar/` and `hive-dictation/`. That is a daemon-side change, so `DaemonContract` goes to 41 with a history entry.
     - Why not name the conn `hivebar/…` to ride the existing rule: it is a lie in every daemon log, and the next headless client would copy it.
   - `dictation.ApplySessionEvent` is extracted from hivebar's `applySessionEvent`, and both clients use it.

2. **Windows host in `cmd/hivegui`.**
   - Starts only on Windows, from `startup`, and stops in `shutdown` via `HUD.Shutdown()`, which cancels a Hive-started Handy recording.
   - Every GUI process runs the host: one goroutine pinned with `runtime.LockOSThread`. It owns a hidden message window and the box window, and runs one `GetMessageW` loop.
   - The hotkey uses `RegisterHotKey(hwnd, id, mods|MOD_NOREPEAT, vk)`. The decisions (register, retry while losing, re-register on a chord change, unregister on disable) live in a pure `dictation.Owner` over a fake-able registrar, so CI covers them. The Win32 calls are a thin adapter. A log line after 30 straight failures notes the chord may be held by another app, since a refusal is indistinguishable from another Hive window owning it.
     - The first process to register wins.
     - The others get `ERROR_HOTKEY_ALREADY_REGISTERED` and retry on a 2 s `SetTimer`, so closing the owning window hands the chord to another window within about 2 s. No named mutex: the OS registration is itself the lock, and it is released when the owner dies.
     - The retry happens only while a chord is wanted. A chord change re-registers. Disabled means unregister.
   - Each process runs its own `dictation.Conn`, so whichever process wins needs no hand-off. A loser's HUD never opens, because only the owner gets `WM_HOTKEY`.
   - The wire stays macOS key codes (kVK) plus Carbon modifier bits, so the daemon, `dictation.json` and the GUI-to-daemon path are unchanged. A pure, untagged Go table maps them to Windows codes:
     - kVK to Windows virtual-key codes (VK);
     - Carbon bits to `MOD_*`: Control to CONTROL, Option to ALT, Shift to SHIFT, Cmd to WIN.
   - The box is a `WS_POPUP` window with `WS_EX_TOPMOST|WS_EX_TOOLWINDOW`, centred on the monitor under the cursor and DPI-scaled. It holds:
     - a target line: `To: <label>  (n of m)`;
     - a multi-line `EDIT`;
     - a status line;
     - a hints line.
   - It uses the system message font.
   - On `WM_HOTKEY`, the host:
     - remembers `GetForegroundWindow()`;
     - calls `HUD.Open()`;
     - `View.Show(focus)` then runs `SetForegroundWindow` plus `SetFocus(edit)`. The hotkey grants foreground rights.
   - `Hide(restore)` hides the box and gives focus back to the remembered window.
   - The `EDIT` is subclassed, and key handling is a pure, untagged `editKeyAction(vk, shift, ctrl, alt)` so it can be tested anywhere:
     - Enter submits;
     - Shift+Enter inserts a newline;
     - Esc cancels;
     - Up/Down call `HUD.Move(∓1)`.
   - `WM_PASTE` sets a one-shot flag, then `EN_CHANGE` calls `HUD.TextChanged(text, pasted)`. Handy pastes with a synthesized Ctrl+V, which reaches the `EDIT` as `WM_PASTE`. Programmatic `SetWindowText` (the clear on open) is suppressed with a guard flag, matching macOS.
   - Calls into the window from other goroutines go through `PostMessage(WM_APP+n)`, with the state copied under a mutex, so `View` methods return immediately as `dictation.View` requires.

3. **Frontend and settings.**
   - `lib/platform.ts` gains `detectWindows`/`isWindows`, and a `dictationSupported = isMac || isWindows`.
   - `bindings.ts`: `dictate-hud` becomes `{ mac: 'Ctrl+Alt+[Space]', other: 'Ctrl+Shift+[Space]' }`.
     - On Linux the binding exists but nothing registers it, and it is hidden from Settings and Help by the `dictationSupported` gates.
     - Its `run` still declines, so in-app presses pass through as today.
   - `carbon-keys.ts`: `carbonHotkey(chord, mac = true)` parses with the caller's platform grammar.
   - `global-hotkey.ts` swaps `isMac` for `dictationSupported` in `reportFocus`, `reconcile` and `init`, and passes `isMac` to `chordsOfIn` and `carbonHotkey`.
   - `shortcuts.ts` shows the "Global (system-wide)" and HUD key groups when `dictationSupported`, with the HUD key names chosen by platform.
   - `Settings.tsx` adds a Windows-only `dictation` tab holding the same `#settings-handy-auto-start` switch, shown when `handyInstalled && handyAutoStart !== null`. On macOS the switch stays in the Menu bar tab.
   - `HandyInstalled()` gets a Windows implementation via `dictation.HandyBin()`.

**Handy CLI risk (spec fallback).**
- Before any Handy code is written, the plan's first implementation step installs Handy v0.10.1 in the Windows VM and probes four things:
  - the install path;
  - whether `--toggle-transcription` and `--cancel` forward to the running instance;
  - whether either flag launches a stopped Handy;
  - **how Handy inserts text on Windows by default** (Ctrl+V, Shift+Insert, or direct typing).
- The HUD recognises a transcription by a paste or by a jump of 2+ runes (hud.go:496-509). Typing arrives as one `WM_CHAR` at a time. If the default is Shift+Insert, the subclass treats it like `WM_PASTE` (both route through `WM_PASTE` in a standard EDIT). If the default is direct typing, the README says Handy's paste method must be clipboard-based, and the box shows that in the status line on open, only when Handy auto-start is on and Handy is installed.
- If the flags don't work, the Handy auto-start option is compiled out on Windows: `HandyInstalled` returns false and the HUD gets a nil `HandyCtl`. The evidence is recorded in the PR, and the box still ships.
- If the VM can't record (no mic, or emulation), the operator runs the Handy rows on their machine.

### Files to change

1. `cmd/hivebar/hud.go`, `hotkey.go`, `handy.go`: delete what moves; keep thin host glue. `hud_native.go` implements `dictation.View`/`Registrar` and calls exported HUD methods.
2. `cmd/hivebar/main.go`: construct `dictation.NewHUD(view, client, dictation.NewExecHandy(nativeHandyRunning), dictation.KeyNames{"↩","⇧↩"})`.
3. `cmd/hivebar/client.go`:
   - `hudEvents`/`hotkeyEvents` become the dictation types;
   - `applySessionEvent` calls `dictation.ApplySessionEvent`.
4. `cmd/hivebar/model.go`: `LabelIn` delegates to `dictation.Label`.
5. `cmd/hivebar/actions.go`: drop `isExecutable`/`runWithTimeout` if no other caller remains (menu.go:111 still uses `runWithTimeout`, so keep it).
6. `cmd/hivebar/hud_test.go`, `hud_handy_test.go`, `hotkey_test.go`: move into `internal/dictation` with package-name edits. The glyph-label tests stay in hivebar. The `/bin/sh` execHandy test becomes a fake `runner` test, plus a `runtime.GOOS != "windows"` real-exec test.
7. `cmd/hivegui/main.go` / `app.go`: start and stop the Windows host from `startup`/`shutdown`, via a no-op `dictationHost` on other OSes.
8. `cmd/hivegui/handy_darwin.go` → use `dictation.HandyBin()`. `handy_other.go` tag becomes `!darwin && !windows`.
9. `cmd/hivegui/frontend/src/lib/platform.ts`, `lib/bindings.ts`, `lib/carbon-keys.ts`, `lib/shortcuts.ts`, `app/global-hotkey.ts`, `components/modals/Settings.tsx`: as in the Approach.
10. Frontend tests (plus `test/dom/keymap-parity.test.ts:371-383`: approval `H` covers dictate-hud only on mac, so add the non-mac Ctrl+Shift+Space case; re-check `test/unit/keymap-import.test.ts:158-165`):
    - `test/dom/global-hotkey.test.ts`
    - `test/unit/carbon-keys.test.ts`
    - `test/dom/settings.test.tsx`
    - `test/dom/settings-shortcuts.test.tsx`
    - `test/unit/shortcut-labels-parity.test.ts`
    - `test/e2e/settings.spec.ts`
    - `test/e2e/every-shortcut.spec.ts` (`dictate-hud` still declines on both platforms; confirm the `other` press is harmless)
    - `test/e2e/wails-mock.ts` (no change expected)
11. `cmd/hivegui/testdata/menu-default-accelerators.json`: no change. dictate-hud has no menu item.
12. Docs:
    - `README.md`: the dictation section and the Keybinds table row become macOS + Windows, and say where the Handy switch lives on each.
    - `AGENTS.md`: the hard-coded HUD keys apply to both native boxes.
    - `DESIGN.md`: the new `internal/dictation` package row; hivebar and hivegui (Windows) host it.
    - `docs/plugins.md:314-315,379-380`: "menu bar's dictation HUD" becomes "the dictation box".
    - `cmd/hivebar/README.md`.
13. `.changesets/575-windows-dictation.md` (added, minor).
13b. Stale comments: `src/app/commands.ts:180-185`, `test/dom/attention-jump-integration.test.ts:342`, `cmd/hivegui/handy_other.go:9`.
13c. `internal/daemon/planreview.go` (`canAnswer` headless set), `internal/buildinfo/contract.go` (41 + history entry), `cmd/hivebar` `hudContract` stays 40 (hivebar needs nothing new). The Windows host reads `DaemonContract` from WELCOME. Below 41 it closes the conn and backs off for 60 s rather than redial-looping. An old daemon briefly counts it as an answerer, which is harmless. Test: `TestConnBacksOffOnOldContract`.
14. `site/features.json`: a new entry or an extended one ("Dictate into a session from any app, on macOS and Windows"), `since: "Unreleased"`.

### New files

- `internal/dictation/hud.go`: the moved state machine.
- `internal/dictation/hotkey.go`: the moved hotkey controller.
- `internal/dictation/handy.go`: `HandyCtl`, `ExecHandy` (via `proc.CommandContext`) and `HandyBin`.
- `internal/dictation/handy_darwin.go`, `handy_windows.go`, `handy_other.go`: the paths and "running" probe per OS.
- `internal/dictation/conn.go`: the dedicated control connection and frame routing. `ApplySessionEvent`.
- `internal/dictation/label.go`: `Label`.
- `internal/dictation/winkeys.go`: untagged kVK→VK and Carbon→MOD tables, plus `editKeyAction`.
- `internal/dictation/*_test.go`: the moved tests plus the new ones below.
- `cmd/hivegui/dictation_windows.go`: the host: thread, hotkey owner/retry, box window, view.
- `cmd/hivegui/win32_windows.go`: the user32/kernel32 proc bindings the host needs.
- `cmd/hivegui/dictation_other.go`: no-op host.
- `cmd/hivegui/handy_windows.go`: `HandyInstalled` via `dictation.HandyBin()`.

### Tests

**Go**, run on all three CI legs unless noted:
- Moved unchanged (`internal/dictation`): every test in `hud_test.go` and `hud_handy_test.go` (around 40 HUD and Handy state tests), and the controller tests in `hotkey_test.go`.
- `TestHintsUseInjectedKeyNames`: Windows `KeyNames` produce `[Enter] send · [Shift+Enter] new line…` and statuses with no `↩`.
- `TestVKForEveryKVK`: every kVK the frontend's `CODE_TO_KVK` can emit maps to a non-zero VK. The list is mirrored in a Go table, with a comment pointing at carbon-keys.ts.
- `TestWindowsModsFromCarbon`: each Carbon bit maps to its `MOD_*`, and unknown bits are rejected.
- `TestEditKeyAction`: Enter → submit, Shift+Enter → newline, Esc → cancel, Up/Down → move, and anything else → default.
- `TestConnRoutesFrames`, over `net.Pipe`:
  - SESSIONS + PROJECTS reach `HUD.SetSessions`;
  - SESSION_EVENT added/removed/updated patch the list;
  - SENT → `OnSent`;
  - ERROR with a session id → `OnError`, without one → ignored;
  - DICTATION → `SetHotkey` + `SetHandyAutoStart`;
  - EOF → `Disconnected` and a redial.
- `TestDialNeverSpawns` (cmd/hivegui): a dead socket path makes the dictation dial fail, and the spawn hook is never called.
- `TestCanAnswerExcludesHeadless` (internal/daemon): `hivebar/x` and `hive-dictation/x` give false, `hivegui/x` gives true.
- `TestE2E_DictationConnIsNotAnAnswerer` (cmd/hived): with only a `hive-dictation/` control client connected, a plan-review park sees zero answerers and a worktree-setup park fails fast (#451).
- `TestOwnerRetriesWhileLosing`, `TestOwnerUnregistersOnDisable`, `TestOwnerChordChangeWhileLosing`, `TestOwnerTakesOverWhenFree`: the single-owner logic over a fake registrar.
- `TestExecHandyTimeout` (non-Windows real exec): a stub that sleeps past the deadline returns a DeadlineExceeded-wrapped error within deadline + WaitDelay.
- `TestApplySessionEvent`: the add/remove/update/unknown-kind table, moved from hivebar's coverage.
- `TestHandyPathsWindows`: from a fake getenv, the candidate list contains the expected paths in order. Pure, runs anywhere.
- `TestExecHandyUsesRunner`: a fake runner records `--toggle-transcription` and `--cancel`, and does not run when not running (`errHandyNotRunning`).
- `TestNoDirectExecOnWindows` (existing) must stay green with the new files.
- hivebar's existing `client_test.go` routing tests stay green.

**Frontend:**
- `global-hotkey.test.ts`:
  - "windows reconciles Ctrl+Shift+Space to CONTROL|SHIFT kVK 49";
  - "linux never calls SetGlobalHotkey";
  - "windows reports the focused session".
- `carbon-keys.test.ts`: "carbonHotkey parses PC grammar when mac=false" (Ctrl stays CONTROL; Mod maps to Ctrl).
- `settings.test.tsx`:
  - "Dictation tab shows on Windows when Handy is installed";
  - "absent on Linux";
  - "absent on Windows without Handy";
  - "macOS keeps the switch in the Menu bar tab".
- `settings-shortcuts.test.tsx`: the dictate row is listed on Windows and hidden on Linux.
- `shortcut-labels-parity.test.ts`: the dictation groups are expected for mac and windows.
- `e2e/settings.spec.ts`: "Windows Dictation tab Handy round-trip" (`navigator.platform='Win32'`, `/?handy=1`).

**Windows VM (manual rows; evidence in the PR):** one row per spec success criterion:
1. Box opens unfocused, with focus.
2. Typed text and Handy text land in it.
3. Two windows open: one box. Close the owner: the other window takes the chord within about 2 s.
4. No GUI running: nothing.
5. Target shown; ↑/↓; Enter delivers to that session only. Checked by a marker command in a shell session and an agent session.
6. Esc writes nothing.
7. No sessions: empty state.
8. Rebind in Settings → Shortcuts, and the new chord works.
9. Dictation tab presence with Handy installed and uninstalled.
10. Handy auto-start rows (start, Handy-key stop + review, Enter stop + send, Esc cancel, option off).
10b. Handy with its *default* paste setting: the transcription lands in the box and is recognised (Enter-stop sends it).
11. macOS smoke: ⌃⌥Space still opens the menu bar's box.

## Verification

- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/dictation/... ./cmd/hivebar/... ./cmd/hivegui/... ./internal/proc/...`
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done`
- `GOOS=windows GOARCH=amd64 go test -c -o /dev/null ./internal/dictation/ ./cmd/hivegui/` (Windows code compiles, tests included)
- `cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && npx vitest run && CI=1 npx playwright test test/e2e/settings.spec.ts test/e2e/every-shortcut.spec.ts`
- `scripts/test.sh`
- `./build.sh --platform windows`, then the VM rows above via the `windows-vm` MCP.
- `./build.sh`, then the macOS smoke: ⌃⌥Space box + Handy, using an isolated hivebar as in the 573 smoke.
- Run the new keymap-parity, Linux-gate and canAnswer tests before the change, and confirm they fail.
- Non-vacuity:
  - `TestVKForEveryKVK` fails if a kVK is missing.
  - The global-hotkey Windows test fails while the `isMac` gate remains.
  - The settings Windows tab test fails without the new tab.

## Open questions / risks

- **Handy's Windows CLI is beta.** Mitigated by the probe-first step and the spec fallback.
- **Foreground rights.** `SetForegroundWindow` from a `WM_HOTKEY` handler is normally allowed. If it's denied, the box shows but has no focus and the paste goes elsewhere. The VM row 1 checks this. Fallback: an `AttachThreadInput` with the foreground thread.
- **DPI.** Wails sets per-monitor DPI awareness. The box scales with `GetDpiForWindow`.
- **A chord taken by a non-Hive app.** Every window retries silently, and the box never opens. Not covered by the spec. Settings shows no "taken" note on Windows (macOS shows one in the menu). Possible follow-up.
- **Windows code isn't staticchecked in CI** (only the Linux leg runs staticcheck). Covered by running `GOOS=windows staticcheck` locally.
- **Size.** About 900 lines move unchanged and roughly 700 are new (Windows host plus bindings). The move is one commit and the Windows host another, so review can tell them apart.
- **DaemonContract 41.** Required by the `canAnswer` change. A GUI built here against a contract-40 daemon offers the daemon restart, as designed.
- **Wire default ⌃⌥Space.** On a fresh Windows state dir, hosts briefly register Ctrl+Alt+Space until the first window's frontend reconciles to Ctrl+Shift+Space, which happens at boot. Accepted. Special-casing the default would break a user who deliberately chose Ctrl+Alt+Space.
- **The conn read loop must not block.** HUD methods take only `h.mu` and hand Handy work to its worker, so a slow Handy can't stall frame reads into the daemon's slow-client hang-up.

## Second opinion

- Independent reviewer (general-purpose agent), 2 rounds.
- **Round 1:** revise, 7/10. Five must-fix items, all applied:
  - the dictation conn dials without spawning;
  - `canAnswer` excludes `hive-dictation/`, with DaemonContract 41;
  - the keymap-parity test;
  - ExecHandy keeps WaitDelay, stderr and deadline semantics;
  - the probe records Handy's Windows paste method.
- **Round 2:** approve, 8/10. Its nice-to-haves were applied (old-contract back-off, worktree-park e2e, conditional status copy). The contract-bump-vs-`hivebar/`-name choice was surfaced to the operator, who approved option A, the bump.

## Decision log

- **2026-11-21** — One spec covers both the box and Handy. Why: operator choice at brainstorm.
- **2026-11-21** — The hotkey works only while a GUI window runs, with no Windows tray agent. Why: operator choice; smaller.
- **2026-11-21** — Default chord is Ctrl+Shift+Space. Why: Ctrl+Alt is AltGr on many layouts.
- **2026-11-21** — If Handy's CLI is unreliable on Windows, hide the option and still ship the box. Why: operator choice.
- **2026-11-22** — Extract the box state machine (hivebar hud.go plus the neutral parts of handy.go and hotkey.go) into a shared package that both hivebar and hivegui host. Why: operator choice; one state machine with no drift. A cmd/ package can't be imported.
- **2026-11-22** — On Windows the Handy switch lives in a new "Dictation" Settings tab. On macOS it stays in the Menu bar tab. Why: operator choice.
- **2026-11-22** — Verify Handy on the VM first. If the VM can't record, the operator runs a Handy checklist on their own Windows machine. Why: operator choice; the VM is ARM with x64 emulation and may have no mic.
- **2026-11-22** — Assumptions (not contested):
  - the wire stays kVK/Carbon and Windows maps it to VK/MOD_* in Go;
  - `RegisterHotKey` failing with 1409 acts as the owner lock, with losers retrying;
  - the box is a native Win32 window on a locked OS thread;
  - the Ctrl+Shift+Space default and the Settings row apply on Windows only, with no Linux default.
- **2026-11-22** — `canAnswer` excludes headless `hive-dictation/` clients, and DaemonContract goes to 41. Why: operator approved option A over naming the conn `hivebar/…`. A clean rule beats a misleading name, and the restart cost on upgrade was accepted.
- **2026-11-22** — Handy probe on Windows 11 ARM VM, Handy 0.10.1:
  - **Install paths.**
    - The NSIS `setup.exe` installs per-user to `%LOCALAPPDATA%\Handy\handy.exe`, with an HKCU Uninstall entry whose `DisplayIcon` points there.
    - The MSI lays out `PFiles\Handy\handy.exe`, which is `%ProgramFiles%\Handy\handy.exe`.
    - `%LOCALAPPDATA%\Programs\Handy` is not used, so it is dropped from the candidate list.
  - **Flag forwarding.** `--toggle-transcription` and `--cancel` forward to the running instance. Its log shows `TranscribeAction::start` and `Initiating operation cancellation`. Both exit 0 in about 1.1 s, and only one handy process remains.
  - **Stopped Handy.** `--cancel` on a stopped Handy launches it, as on macOS, so the "is running" check is required.
  - **Paste.** The default `paste_method` is `ctrl_v`, which arrives in a standard EDIT as `WM_PASTE`, so no direct-typing fallback is needed for the default.
  - **Recording.** The VM can't record: there is no model (the VM has no internet) and only a Line In. Per the decision above, the Handy recording rows go to the operator's machine. The VM covers the CLI rows and paste detection via a synthesized Ctrl+V.

## Progress

- **2026-11-21** — Spec and issue #575 created via /hs-brainstorm; triaged L/P2; plan created at RESEARCH.
- **2026-11-22** — Research done. Plan approved via plan-html (round 1, about 13 s); stage IMPLEMENT.
- **2026-11-22** — Windows VM verification (Windows 11 ARM, amd64 build `a0c87315`+fix, via the windows-vm MCP):
  1. Hotkey and focus. With Edge in front, Ctrl+Shift+Space opened the box with keyboard focus. Text placed by a Ctrl+V paste landed in it.
  2. Delivery. Enter sent the text to the target session (`main`) only: `%TEMP%\dict575.txt` contained `DICT575`. The box closed and focus went back to Edge.
  3. Box keys.
     - **Down** moved the target to `fern-brook (2 of 2)`.
     - **Shift+Enter** added a line.
     - **Esc** closed the box with nothing written: `esc575.txt` was absent.
  4. Two windows. With two hivegui processes, one press opened exactly one box, owned by the first window. After the first window closed, the next press opened a box from the second process: a new HWND.
  5. No GUI. With no hivegui running, the chord opened nothing.
  6. Settings.
     - With Handy installed, a **Dictation** tab appeared next to Shortcuts, holding the switch. Its hint read "Ctrl+Shift+Space … Enter stops Handy".
     - The switch wrote `handy_auto_start` to `dictation.json`.
  7. Handy, switch on. Opening the box showed "Recording with Handy…" and Handy logged `TranscribeAction::start`. Esc made Handy log `Operation cancellation completed`.
  8. Handy, switch off. Opening the box added no new Handy log line.
  9. Rebind. In Settings → Shortcuts, Ctrl+Shift+F12 replaced the chord: the old chord stopped opening the box and the new one opened it. Resetting the row restored `key_code 49, modifiers 0x1200` in `dictation.json`.
  - **Bug found and fixed.** `GetKeyState` still reported the hotkey's own Ctrl/Shift as held, because they were released while another app was in front. Plain Down and Esc read as modified and did nothing. Switched to `GetAsyncKeyState`; Down and Esc then worked.
  - **Not run on the VM:**
    - a real Handy transcription: there is no model and no internet. That is an operator row.
    - the "no live sessions" empty state. It is shared HUD code, covered by `TestHUDNoSessionsEnterInert` in internal/dictation.

## PR convergence ledger

- **2026-11-22 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 91895bac.

## Gate verdict

## Open questions
