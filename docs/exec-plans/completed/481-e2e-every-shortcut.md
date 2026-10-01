# E2E-test every keyboard shortcut and menu command from the binding data

- **Spec:** [docs/product-specs/481-e2e-test-every-keyboard-shortcut-and-menu-command.md](../../product-specs/481-e2e-test-every-keyboard-shortcut-and-menu-command.md)
- **Issue:** #481
- **Status:** completed
- **PR:** #486
- **Branch:** feature/481-e2e-every-shortcut

## Summary

A Playwright spec on the mock-bridge layer that reads `KEY_SCOPES` and `MENU_COMMANDS` from the running app, fires every chord and menu event on both the mac and non-mac binding sets, and asserts through a test-only command log that the expected command ran (or declined). Real-effect spot checks cover the shortcuts no e2e test presses today.

## Research

All frontend paths are relative to `cmd/hivegui/frontend/`.

### Relevant code

- `src/app/key-scopes.ts:405-425` — `KEY_SCOPES`, 19 scopes in precedence order. `Binding {keys, command: string|null, repeat?: false}` (:37-44); `command: null` is reserved. `modal()` helper (:70-83) makes a scope active when `isModalOpen(id)`. `APP_BINDINGS` :281-380 (~50 bindings); `plugins` scope :389-403 builds `plugin:<pluginId>:<cmdId>` bindings dynamically. `matchBinding` :441, `scopeById` :455.
  - Scopes with bindings: inline-rename(1), choice-dialog(1), command-palette(1), settings(2), worktrees(2), quick-idea(3), idea-inbox(3), help-overlay(2), help-modal(2), whats-new(1), plugin-view(1), build-log(1), blocked-tile(1), dead-overlay(3), app(~50), plugins(dynamic). Zero bindings: find-box (text-input), launcher, project-editor.
  - The same chord means different commands per scope (⌘I, ⇧⌘I, ⌘E, ⌘,, ⌘/), so each scope's chords must be fired with that scope on screen.
- **`key-scopes.ts` cannot be imported from the Playwright (Node) process**: it imports the store, terms, plugin-host and modal modules. The binding data has to be read inside the page.
- `src/lib/chord.ts` — pure parser. `parseChord(s, isMac)` :49 returns `{meta, ctrl, alt, shift: boolean|'any', key?, code?}`; `chordsFor(keys, isMac)` :79 resolves `Keys` (string | string[] | `{mac?, other?}`) per platform; `[Code]` tokens match `e.code`; `Any+` / `Shift?` mark optional modifiers.
- `src/lib/platform.ts:10-21` — `isMac = detectMac(navigator)` once at import, from `userAgentData.platform || platform`.
- `src/app/keyboard.ts:29-92` — `runBinding`: `null` → return, no preventDefault; otherwise `declined = !runCommand(id)` and the key is consumed (`preventDefault` + `stopPropagation`) unless declined. `dispatchKey` walks `KEY_SCOPES`; it is a **window capture** listener, so a synthetic `KeyboardEvent` dispatched at `document.body` goes through the real dispatch path and its `dispatchEvent` return value reports consumption.
- `src/app/command-registry.ts:58-65` — `runCommand(id): boolean`, the single choke point for keydown (keyboard.ts:33), menu (commands.ts:377) and palette (modals/command-palette.ts:46). Leaf module, no app imports. Missing id → `console.warn` + `false`.
- `src/app/commands.ts:329-380` — `MENU_COMMANDS`: 37 named events + `menu:switch-1..9` = 46. Two ids differ from the event name (`menu:move-session-forward`→`move-forward`, `…-backward`→`move-backward`). Subscribed with `EventsOn`, return value discarded — so the menu path is only observable from inside `runCommand`.
- Known decliners: `focus-active-session` in single view (commands.ts:207), `switch-N` past the last session (:269-270), grid-left/right and arrow-shift-* when `handleArrow` returns false (:287-290).
- Side-effecting commands under the mock: `delete-project` (mock `Confirm` returns true → project deleted), `close-session`, `restart-hive` (Confirm true), `reload-gui` (sets a module-level `daemonRestarting` flag that silently disables later reload/restart), `toggle-scroll-debug` (flips localStorage `hive.debug`, then **`location.reload()`**), `new-window` / `open-os-terminal` / `close-window` (mock no-ops at wails-mock.ts:1058/1125/1062 — **no call is recorded today**).
- `src/store/store.ts:1545-1551` — the existing test-only gate: `import.meta.env.VITE_WAILS_MOCK === '1' || VITE_WAILS_REAL === '1'` → `window.__hive_state`. Vite inlines both to literals, so the block is dead in `npm run build`. Window globals typed in `src/globals.d.ts`.
- `test/e2e/wails-mock.ts` — vite-substituted bridge (`vite.config.js` `hive-wails-substitute`). `window.__hive` (:1748-1970, typed in `test/e2e/hive-global.d.ts`): `emit(name, …args)` fires `EventsOn` listeners synchronously and swallows exceptions; seeding (`addSession`, `seedWorktrees`, `killSession`, `state` mutation + `emit('session:event', …)`); narrow call logs (`openFileCalls`, `openedUrls`, …). Seed state: project `p1`, session `s1`.
- `playwright.config.js` — vite **dev server** (`VITE_WAILS_MOCK=1`, port 5174), single chromium project, 30 s test timeout, `fullyParallel: false`, CI retries 1 + `failOnFlakyTests`.
- `test/e2e/file-links.spec.ts:16-21`, `find-in-session.spec.ts:15`, `keymap-activity.spec.ts:11` — `addInitScript` overriding `navigator.platform` and `userAgentData`. No spec forces mac today.
- `test/e2e/plugin-helpers.ts` — `boot`, `installPlugin(page, 'session-notes')` (repo plugin binds `Mod+Shift+O`, `plugins/session-notes/ui.mjs:161`); its `MOD` follows the **host**, not the page platform.
- Per-scope fixture routes that exist today: settings (`menu:settings`), worktrees (`Mod+E`, `seedWorktrees`), worktree inline rename and choice dialog (`focus-traps.spec.ts:73-86, 243-257`), dead overlay (`alive=false` + `session:event updated`, `ux-polish.spec.ts:350-400`), plugin (`installPlugin`), build-log (`emit('update:progress', …)`, `build-log.spec.ts`). **Blocked tile has no e2e path**: set `phase:'blocked'` + `pending_worktree_choice` on a mock session, emit `updated`, dismiss the auto-raised choice dialog (events.ts:359) with Escape.
- Existing unit coverage: `test/dom/keymap-parity.test.ts` (chord → id oracle), `test/dom/key-scopes.test.ts` (decline/reserved/throw consumption), `test/dom/commands.test.ts:98-125` (every MENU_COMMANDS id resolves and is subscribed). Local `codeFor` key→`e.code` table at keymap-parity.test.ts:211-229.
- CI: `.github/workflows/ci.yml:72-93` runs the mock e2e layer on **Linux, macOS and Windows** (the spec text says Linux; the platform override makes the sweep host-independent). `ci.yml:119-126` builds the production frontend (`npm run build`) on every leg. No check greps `dist/` for test hooks today. Current mock e2e step: ~10.5–11.6 min per leg, unsharded.

### Constraints / dependencies

- Binding data must be pulled from the page (in-page `import('/src/app/key-scopes.ts')` through the vite dev server, which should yield the same module instance the app loaded; fallback is exposing it through the gated test hook). Playwright cannot generate one `test()` per chord at collection time from data it only has at run time, so tests are per (platform × scope) and per (platform × menu), with `test.step` per chord.
- Commands with destructive or navigating effects (`delete-project`, `toggle-scroll-debug`'s reload, `reload-gui`'s sticky flag) require isolation between chords.
- Spec Non-goals: no change to bindings, commands or dispatch behaviour. A log append inside `runCommand` behind a build-time-dead gate is the only allowed product-code touch.

### Prior lessons

- CI sets `failOnFlakyTests`, so a single Linux flake is a hard red; prefer deterministic waits over timing.
- After changing anything key-related, grep the tree for comments describing the old routing (`lib/shortcuts.ts:8-16` lists the drift surface).
- xterm encodes many keys itself; unconsumed/reserved keys reach the PTY through xterm's `evaluateKeyboardEvent`.

### Conventions card

From `AGENTS.md`:

- Build: `./build.sh` (macOS .app); frontend `npm run build` in `cmd/hivegui/frontend`.
- Tests: `scripts/test.sh` (layers `go · unit · dom · e2e`); this change uses `scripts/test.sh unit dom e2e`.
- Lint: `npx biome ci .` and `npm run typecheck` in `cmd/hivegui/frontend` (typecheck needs `./scripts/ci-bootstrap.sh` in a fresh worktree); `scripts/ui-lint.sh`.
- TDD: every behaviour change ships its test; "boil the lake" — apply low-risk nits in the same PR.
- Local Playwright must run with `CI=1`, or it reuses a stale vite dev server.
- Test-only change: no changeset needed for test files; the `runCommand` gate touches `src/` but has no user-visible effect → `no-changeset` label. No `DaemonContract` bump (frontend only).

## Approach

A table-driven Playwright spec (`test/e2e/every-shortcut.spec.ts`) reads the binding data **inside the page**: it calls `import('/src/app/key-scopes.ts')`, `/src/app/commands.ts` and `/src/lib/chord.ts` through the vite dev server. A probe confirmed three things:
- The import returns the same module instance the app loaded: `scopeById('settings').active()` is true after `menu:settings`.
- The navigator override flips `isMac`.
- A boot takes about 0.2 s.

The spec then fires **every chord string** of every binding, on both platforms, plus every `MENU_COMMANDS` event. It asserts against a test-only command log recorded inside `runCommand`, the one choke point that keydown, the menu and the palette share.

**Isolation.** A `page.addInitScript` clears `localStorage` and `sessionStorage` before any app module runs, on every navigation. Without it, `hive.view`, `hive.debug`, zoom and sidebar prefs would leak from one chord into the next, so a `goto` alone would not give a fresh page. `bootWithSessions` then asserts the starting state before any fixture runs:
- `view === 'single'`
- `sessions.length === SESSION_COUNT`
- active session is `s1`

**Firing (operator decision: synthetic events).**
- Each chord is turned into a `KeyboardEvent` init in the page:
  - `parseChord(s, isMac)` supplies the modifiers. `'any'` becomes off.
  - `key` comes from the chord, with named keys re-cased (`escape`→`Escape`, `arrowup`→`ArrowUp`, …). An unknown multi-character key throws, so a new key forces a table update.
  - `code` comes from the chord, or is derived from the key (`KeyJ`/`j`, `Minus`/`-`, `Backquote`/`` ` ``, `DigitN`).
- The event is dispatched at `document.body` with `bubbles: true, cancelable: true`. `cancelable` is required: without it `!dispatchEvent()` is always false.
- **Before firing, the same evaluate checks that the event resolves to the binding under test.** To stop the test's walk from drifting from the real one, `keyboard.ts` exposes the resolution half of `dispatchKey` as `resolveKey(e, mac): {scope, binding} | 'text-input' | undefined`, and `dispatchKey` calls it. This is a pure extraction with no behaviour change: a `text-input` scope passes the key through, a `matched` scope with no match continues, and an `exclusive` scope stops (and traps focus). The sweep calls `resolveKey` in the page and asserts that it returns this scope and this binding. This catches synthesizer bugs (a wrong key or code for a chord) as a precise failure. It also makes the reserved-chord assertion non-vacuous, because the null binding must actually match. A chord shadowed by a higher-precedence binding fails here. The fire and the log read happen in **one** `page.evaluate`, so `toggle-scroll-debug`'s `location.reload()` cannot lose the result.
- `!dispatchEvent(...)` is "consumed".

**Per chord, with a fresh page (operator decision).** Each chord goes through these steps:
1. `goto('/')`, then seed to `SESSION_COUNT = 3` sessions in single view.
2. Apply the scope fixture, then assert `scopeById(id).active()`.
3. Clear the log, fire the chord.
4. Assert:
   - `command === null` (reserved): log empty and not consumed.
   - Otherwise:
     - `log[0]` deep-equals `{id: binding.command, ran: expected}`.
     - `consumed === ran`.

`expected` is computed from documented rules, not measured and pinned. Pinning measurements would turn today's bugs into expectations.
- `switch-N` → `N <= SESSION_COUNT`.
- `DECLINES_IN_SINGLE_VIEW` is exactly three commands:
  - `focus-active-session` (commands.ts:207)
  - `grid-left` and `grid-right`: `handleArrow` (actions.ts:180-191) returns false only for horizontal moves in single view.

  `arrow-shift-*` do not decline. Every fixture runs in single view.
- Everything else → `true`.

**Postconditions, generated from the data, so that `ran:true` is not vacuous for void commands.** After the fire:
- **Closers.** If the command id ends in `.close`, `.dismiss` or `.cancel`, the scope that owns it must be **inactive**, measured with `scopeById(id).active()`. This covers every modal closer, `inline-rename.cancel`, `choice-dialog.dismiss`, and `dead-session.close` / `dead-session.dismiss` (the dead-overlay scope must be inactive).
- **Openers.** If a scope exists whose id equals the command id (`settings`, `worktrees`, `quick-idea`, `idea-inbox`, `command-palette`), that scope must be **active**. Help-related ids map through a small table: `keyboard-shortcuts` and `help-modal.shortcuts` both lead to `help-overlay`.
- **Blocked tile.** `session.answer-worktree-question` must leave the choice dialog open, i.e. the `choice-dialog` scope active.
- **Dead overlay.** `dead-session.restart`: the mock must record a `RestartSession` call. `bridgeCalls` also records `RestartSession`.
- **Everything else** in `app` and `plugins` is proven **dispatch-only** by the sweep. Real effects are covered by the spot checks. A binding remapped to a *different existing* id (e.g. zoom-in → zoom-out) passes the generated sweep by construction. The hand-written oracle `test/dom/keymap-parity.test.ts` catches it, as do the spot checks. The plan states this mapping so the gate can check the "wrongly id'd" criterion against those two.

A binding whose command id is unregistered logs `{id, ran:false}` and fails.

**Non-empty sweep guard.** Each `(platform × scope)` test asserts it fired at least one chord. The only exception is an explicit `EMPTY_ON = { other: [...], mac: [...] }` list, where the data really is empty on that platform. The plugins fixture waits until the plugin status is `'active'` (plugin-host.ts:516-517), then reads the live plugin bindings. Zero bindings there fails.

**Scope coverage is enforced.**
- `FIXTURES: Record<scopeId, fixture>` plus `UNBOUND_SCOPES = ['find-box','launcher','project-editor']`.
- A coverage test enumerates `KEY_SCOPES` in the page and fails on any scope id that is in neither list. It also fails if an `UNBOUND_SCOPES` entry has bindings.
- So a new scope fails until someone writes its fixture.

**Fixtures.** Each uses a user-reachable route where one exists, otherwise the modal module's open function via in-page import:

| Scope | Fixture |
|---|---|
| inline-rename | Double-click a sidebar row |
| choice-dialog | `seedWorktrees` + ⌘E + Delete, per focus-traps.spec.ts:73-86 |
| command-palette / settings / worktrees / quick-idea / idea-inbox | `emit('menu:…')` |
| help-overlay / help-modal / whats-new | The opening command, or `open*()` |
| plugin-view | Open session-notes' view |
| build-log | `emit('update:progress', …)` |
| blocked-tile | Mock session `phase:'blocked'` + `pending_worktree_choice`, emit `updated`, dismiss the auto-raised choice dialog |
| dead-overlay | `alive=false` + `last_error`, emit `updated` |
| app | Base boot |
| plugins | `installPlugin('session-notes')` + close settings, then read the plugin's live bindings |

**Tests are per (platform × scope)**, with `test.step` per chord, and run in describe-parallel mode. The binding list is only known inside the page, so one `test()` per chord at collection time is impossible. `test.setTimeout` is sized to the chord count.

**Menu sweep.**
- Fire all 46 `MENU_COMMANDS` events under the forced-mac platform only; the native menu is darwin-only (menu_darwin.go).
- Use a fresh page per event and assert `log[0]` = `{id, ran: expected}`, plus the same generated opener postconditions.
- **Menu-name parity.** A dom test reads `cmd/hivegui/menu_darwin.go`, extracts every emitted `"menu:…"` literal (`menu:switch-%d` expands to 1..9), and asserts the set equals `Object.keys(MENU_COMMANDS)`. A menu item missing from the table can no longer go untested.
- `menu:switch-N` uses the same `N <= SESSION_COUNT` rule.

**CI legs (operator decision: all three).** The spec runs wherever `scripts/test.sh e2e` runs (Linux, macOS, Windows). Each describe forces its platform with `addInitScript`, as in file-links.spec.ts:16-21. The host OS therefore never decides the binding set.

**Command log.** In `src/app/command-registry.ts`:
```ts
let commandLog: CommandLogEntry[] | undefined;
if (
  typeof window !== 'undefined' &&
  (import.meta.env.VITE_WAILS_MOCK === '1' || import.meta.env.VITE_WAILS_REAL === '1')
) {
  commandLog = [];
  window.__hive_commandLog = commandLog;
}
// runCommand: missing → commandLog?.push({id, ran:false}); otherwise ran = cmd.run() !== false; push; return.
```
- This is a direct `if` block, mirroring `window.__hive_state` (store.ts:1545-1551). Vite inlines the env literals to `false`, so the bundler drops the whole block, and `commandLog` stays `undefined` in production.
- `scripts/check-test-hooks-stripped.sh` proves that the bundler really dropped it.
- A throwing command logs nothing. The spec then fails on a missing entry, which is right: it is broken.

**Real-effect spot checks** use real `page.keyboard.press`, under both forced platforms, with the modifier taken from the forced platform rather than `process.platform`:

| Shortcut | Asserted effect |
|---|---|
| zoom in ⌘= | Terminal font size grows |
| Ctrl+` | Mock `OpenTerminalAt` called with the active cwd |
| back / forward | Active session walks history |
| activity grid | Grid visible |
| ⇧⌘P | Tool chooser opens |
| ⇧⌘N | `OpenNewWindow` called |
| ⇧⌘W | `CloseWindow` called |
| ⇧⌘⌫ | Project removed from the sidebar; mock Confirm returns true |
| ⌘B / ⇧⌘B | Ring the bell on s2, ⌘B activates s2, ⇧⌘B jumps back |
| ⌘[ / ⌘] | With two projects, the active project changes |
| ⌘3–⌘9 | With 9 sessions, each activates session N |
| ⇧⌘Z (reserved) | Active session and view unchanged, no log entry |

**Why this beats the obvious alternative.** The alternative is hand-listing chords in a spec. That drifts from `key-scopes.ts` the moment a binding is added, which is exactly what the issue exists to stop. Importing the data in Node is not possible: `key-scopes.ts` pulls in the store and modal graph.

### Files to change

1. `cmd/hivegui/frontend/src/app/command-registry.ts`: add `CommandLogEntry` and the env-gated `commandLog`, and record each `runCommand` result. Behaviour is unchanged.
2. `cmd/hivegui/frontend/src/globals.d.ts`: declare `__hive_commandLog?: CommandLogEntry[]`.
3. `cmd/hivegui/frontend/test/e2e/wails-mock.ts`: record `OpenNewWindow`, `CloseWindow`, `OpenTerminalAt(dir)` and `RestartSession(id)` in a generic `bridgeCalls` log, exposed as `__hive.bridgeCalls(method?)`. The mock `Confirm` already returns true (wails-mock.ts:1144); the ⇧⌘⌫ spot check depends on that.
3a. `cmd/hivegui/frontend/src/app/keyboard.ts`: extract `resolveKey` from `dispatchKey` and export it (pure refactor). `test/dom/key-scopes.test.ts` / `keyboard-precedence.test.tsx` keep passing unchanged, which is the proof that behaviour did not change.
4. `cmd/hivegui/frontend/test/e2e/hive-global.d.ts`: type `bridgeCalls`.
5. `.github/workflows/ci.yml`: after "Build frontend", add a step that runs `scripts/check-test-hooks-stripped.sh` on every leg, with `shell: bash` (Windows defaults to pwsh; compare ci.yml:117).
6. `AGENTS.md` (Testing Conventions): one bullet saying every binding and menu item is swept automatically by `every-shortcut.spec.ts`, and that a new key scope needs a fixture there.
7. `cmd/hivegui/frontend/src/app/key-scopes.ts` header comment: one line pointing at the e2e sweep (comment only).

### New files

- `cmd/hivegui/frontend/test/e2e/every-shortcut.spec.ts`: the generated sweep (key scopes × platforms, menu events), the scope-coverage test, and the real-effect spot checks.
- `cmd/hivegui/frontend/test/e2e/fixtures/key-sweep.ts`: in-page helpers (`chordEventInit`, `listScopeChords`, `fireChord`, `fireMenu`), `forcePlatform(page, 'mac'|'other')`, `bootWithSessions(page, n)`, and the scope `FIXTURES` map.
- `scripts/check-test-hooks-stripped.sh` (committed with mode 755; resolves `dist` relative to its own location, not the cwd): fails in any of these cases:
  - `cmd/hivegui/frontend/dist` is missing
  - `dist` contains **zero** `.js` files, so an empty dist cannot pass
  - any of them contains `__hive_commandLog` or `__hive_state`
- `cmd/hivegui/frontend/test/e2e/fixtures/key-sweep.ts` loads app modules in the page through a **variable** specifier (`const url = '/src/app/key-scopes.ts'; await import(/* @vite-ignore */ url)`), so `tsc` never tries to resolve an absolute path.

### Tests

- `every-shortcut.spec.ts`:
  - `test('every key scope has a fixture or is declared unbound')`
  - `test.describe('mac' | 'other')` → `test('scope <id>: every chord dispatches its command')` for each scope in `FIXTURES`. Each `test.step` is one chord, asserting log entry, ran/declined and consumed.
  - `test('every MENU_COMMANDS event runs its command')` (mac).
  - `test.describe('spot checks — mac' | 'other')`: `zoom in grows the font`, `Ctrl+backquote opens an OS terminal at the active cwd`, `back/forward walk session history`, `activity grid opens`, `duplicate-choose-tool opens the chooser`, `new window calls OpenNewWindow`, `close window calls CloseWindow`, `delete project removes it`, `next-attention and jump-back`, `prev/next project`, `switch 3–9`, `reserved ⇧⌘Z does nothing`.
- `test/dom/command-registry.test.ts`, or an extension of `commands.test.ts`: `runCommand records {id, ran} when the test flag is set` and `records ran:false for a declining or missing command`. vitest sets neither env var by default, so the test uses `vi.stubEnv` and imports the module fresh.
- `test/dom/commands.test.ts`: `test('every menu event menu_darwin.go emits is in MENU_COMMANDS and vice versa')`.
- `scripts/check-test-hooks-stripped.sh`: the production-bundle proof. CI runs it after `npm run build`.

**Navigation-racing menu events.** Only `menu:toggle-scroll-debug` reloads the page; neither it nor `reload-gui` has a key binding. Its evaluate emits the event and returns the log plus `localStorage['hive.debug']` **synchronously**. `location.reload()` only schedules the navigation, so the evaluate returns first. The test then awaits `load`. If the evaluate still loses the race (context destroyed), the test catches that one error and falls back to awaiting `load`. It then reads `hive.debug`; the storage-clearing init script skips the clear when a `sessionStorage['hive.sweep.keep']` flag is set for this case.

### Verification

Run in `cmd/hivegui/frontend` unless noted:

1. `CI=1 ./node_modules/.bin/playwright test test/e2e/every-shortcut.spec.ts`: all pass.
2. Mutation checks, each must make (1) fail, then revert:
   - Change one binding's command id in `key-scopes.ts` to a non-existent id. Expect a fail.
   - Make `switch-3`'s run return false. Expect a fail.
   - Add a dummy scope to `KEY_SCOPES`. The coverage test must fail.
   - Break the settings fixture so the scope never opens. Expect a fail.
   - Make `closeSettings` a no-op. The closer postcondition must fail.
   - Change the synthesizer's code for `Backquote` to something else. The match check must fail with a precise message.
   - Unbind session-notes' keys. The plugins non-empty guard must fail.
   - Seed `localStorage['hive.view']` to a grid view in a probe run without the clearing init script. `bootWithSessions`' `view === 'single'` assertion must fail.
3. `npm run build && ../../../scripts/check-test-hooks-stripped.sh`: passes. Each of the following must make the script fail:
   - Remove the env gate from `command-registry.ts` and rebuild.
   - Run it against an empty `dist`.
4. `cd ../../.. && scripts/test.sh unit dom e2e`: green; record the e2e wall-time delta.
5. `npx biome ci . && npm run typecheck && ../../../scripts/ui-lint.sh`: green.

### Open questions / risks

- **Decline set.** Expectations come from documented source rules. If a command declines unexpectedly in single view, the spec fails. That is a finding to report, not something to pin.
- **Cost.** Side effects are contained by the storage-clearing init script plus a fresh `goto` per chord. The probe measured ~0.2 s for a bare boot. With fixtures, the estimate is ~300 fires in total, 1–3 min per leg. The measured delta goes in Progress.
- **Real `Meta+…` presses on the Windows and Linux runners** go through CDP, not the OS, so Win and Super interception should not apply. If a leg misbehaves, the spot check for that platform is the thing to look at. Do not loosen the sweep.
- **Spec drift.** The spec says the sweep runs on Linux. The operator chose all three legs, so this is a superset.
- **The plugin scope's binding is the core-overridable `Mod+Shift+O`.** `resolveCommands` may drop plugin chords that collide with core ones. The fixture reads live bindings, so it tests what actually ships.
- **No changeset**: test-only plus an inert gated log. The PR takes the `no-changeset` label.

## Second opinion

- **Round 1: `revise`, confidence 7.** It had 6 must-fix items, and all 6 were applied:
  1. localStorage leaked across chords, so the decline set would have depended on order. Fixed with an init script that clears storage, plus an assertion on the boot state.
  2. `ran:true` was vacuous for void commands. Fixed with generated opener and closer postconditions; the dispatch-only remainder is mapped to keymap-parity and the spot checks.
  3. A scope could sweep zero chords and still pass. Fixed with a non-empty guard and a wait for plugin activation.
  4. Reserved chords passed vacuously when the event matched nothing. Fixed with a resolution check against `resolveKey`.
  5. The CI step needed `shell: bash`, an executable script, and a failure on an empty dist.
  6. Events now carry `cancelable: true`.
- **Round 2: `approve`, confidence 7.** No must-fix items. These nice-to-haves were applied anyway:
  - `DECLINES_IN_SINGLE_VIEW` is pinned exactly; `arrow-shift-*` never declines.
  - `resolveKey` is extracted from `dispatchKey`, so the test cannot drift from the real walk.
  - The `toggle-scroll-debug` reload race now has an explicit fallback.
  - `RestartSession` is recorded in `bridgeCalls`.
  - The opener postconditions also run on menu events.
  - A Go/TS parity test checks menu-event names.
  - The script resolves its paths relative to its own location.
- No injection attempts were found in either round.

## Decision log

- **2026-10-01** — Sweep fires synthetic `KeyboardEvent`s (operator choice); real `page.keyboard.press` only in spot checks. Why: exact event per chord and a direct consumed signal from `dispatchEvent`.
- **2026-10-01** — Sweep runs on all three CI legs (operator choice), each describe forcing its platform. Why: operator preferred uniform coverage over saving the macOS/Windows minutes.
- **2026-10-01** — Fresh page per chord plus storage-clearing init script (operator choice + second-opinion fix). Why: destructive/sticky commands and persisted prefs would otherwise make results order-dependent.
- **2026-10-01** — Read binding data in-page rather than importing `key-scopes.ts` in Node. Why: the module imports the store and modal graph and cannot load outside the browser.
- **2026-10-01** — The menu-name parity check lives in the e2e spec, not `test/dom/commands.test.ts`. Why: Vite denies a `?raw` import from outside the frontend root, and the dom project has no `node:fs` types; the Playwright side runs in Node and reads `menu_darwin.go` directly.
- **2026-10-01** — `menu:worktrees` is allowlisted as `NOT_IN_MENU`. Why: it has been in `MENU_COMMANDS` since #277, but `menu_darwin.go` never emits it (⌘E reaches the webview as a keydown). Fixing it would add a menu item or drop a handler, and the spec's Non-goals forbid both; it is surfaced to the operator instead.
- **2026-10-01** — Added a menu naming rule (`command === event − 'menu:'`, two explicit aliases). Why: a mutation check showed that a remapped menu entry (`menu:zoom-out → zoom-in`) passed the generated sweep.
- **2026-10-01** — Plan said the spec would reuse `plugin-helpers.installPlugin`; the sweep has its own in `fixtures/key-sweep.ts`. Why: the shared one opens Settings with a host-platform key, which a forced-platform page does not bind.
- **2026-10-01** — Cosmetic divergence from the plan's file lists:
  - The fixture helpers are `prepare` / `boot` / `scopeChords` / `fireChord` / `fireMenu` rather than the names the plan sketched.
  - `FIXTURES` lives in the spec, not in `fixtures/key-sweep.ts`, so the coverage test and the sweep read one table.
  - The command-log unit tests extend `test/unit/command-registry.test.ts` instead of a new dom test, because that file already owns the registry.
  - Review iteration 1 added `scripts/check-test-hooks-stripped-selftest.sh`, wired into `changesets.yml` beside the other gate selftests.

  Why: recorded so the plan matches what shipped.

## Progress

- **2026-10-01** — Research complete; stage → PLAN.
- **2026-10-01** — Plan approved via plan-html (round 1); stage → IMPLEMENT.
- **2026-10-01** — Implemented. 218 chord and menu fires (both platforms) plus 24 spot checks, about 45 s locally. Mutation checks were all killed:
  - unknown command id
  - wrong decline
  - new scope with no fixture
  - no-op closer
  - synthesizer key bug
  - leaked view pref
  - unbound plugin
  - menu table drift
  - menu remap
  - env gate removed (bundle check and unit test)
  - empty or missing dist

  A reserved chord remapped to an existing id survives the sweep by construction; `test/dom/keymap-parity.test.ts` kills it.

## PR convergence ledger

- **2026-10-01 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: b601e00a34b378d9c6c0f25c1960a34ba3b2f616dd809f2d47cfad2c1312f5e5; threads_open: 0; action: autofix+push; head_sha: 5867600a.
- **2026-10-01 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 5867600a.

## Gate verdict

- **2026-10-01** — verdict: PASS; phase: —; checks: 7 passed / 0 failed / 0 followups; followups: none; one-line: the generated sweep, command log and bundle check meet all four success criteria; no non-goal touched; docs accurate; CI green on Linux, macOS and Windows.
  - 2026-10-01 dimensions:
    - acceptance — PASS — The local sweep passed: 58 tests. A mutated binding id failed it on both platforms. The bundle check and its selftest pass, and so do the command-registry unit tests. The validator returned NEEDS_FOLLOWUP only because CI was still pending on the bookkeeping head; that CI later finished green on all three legs (head 95d4bdff). Every shortcut listed in the spec's Problem section has a spot check in `every-shortcut.spec.ts`.
    - non-goals — PASS — `resolveKey` was traced branch by branch and is behaviour-identical. The `key-scopes.ts` change is comment-only, `commands.ts` is untouched, and no `e2e-real` or Go menu file changed.
    - doc accuracy — PASS — The `no-changeset` label is justified. The `AGENTS.md` bullet, the code comments and the CI step comments are accurate, and no stale docs were found.

## Open questions
