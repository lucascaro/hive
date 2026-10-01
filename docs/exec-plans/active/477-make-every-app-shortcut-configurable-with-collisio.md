# Make every app shortcut configurable, with collision resolution

- **Spec:** [docs/product-specs/477-make-every-app-shortcut-configurable-with-collisio.md](../../product-specs/477-make-every-app-shortcut-configurable-with-collisio.md)
- **Issue:** #477
- **Status:** active
- **PR:** #487
- **Branch:** feature/477-configurable-shortcuts
- **Phase:** 1 of 3

## Summary

Add a Settings → Shortcuts tab that lets users rebind every app command (core and plugin), resolves collisions per scope, persists custom bindings across reloads/upgrades, and propagates the user's current bindings to every surface (help overlay, palette, native macOS menu, inline hints, tooltips). Builds on the #478 command bus / key-scopes refactor.

## Research

FE = `cmd/hivegui/frontend/src`. Gathered 2026-09-30 by three Explore workers on top of `main@8747db09` (#478 merged).

### Relevant code

**Binding data and dispatch (substrate from #478)**
- `FE/lib/chord.ts:1-87` — chord grammar: `Mod|Ctrl|Alt|Shift`, `X?` don't-care, `Any+`, `[Code]` match on `e.code`; `Keys = string | string[] | {mac?, other?}`; `chordsFor` returns `[]` for a missing platform half. **No chord → display-label formatter exists.**
- `FE/app/key-scopes.ts:37-64` — `Binding {keys, command|null, repeat?}`, `KeyScope {id, active(), owns, trap?, bindings()}`. `command: null` reserves a chord for the terminal (`Mod+Shift+Enter` :337, `Mod+Shift+Z` :365).
- `FE/app/key-scopes.ts:281-380` — `APP_BINDINGS` is a module `const`; `modal()` helper (:70-83) closes over const arrays. **But `bindings()` is a function called on every keydown**, and the plugins scope (:389-403) already computes live. Parsed-chord cache keyed by chord string (:429-438) is safe with new strings.
- `FE/app/key-scopes.ts:405-425` — `KEY_SCOPES` precedence. Exclusive modals shadow everything below. Real collision domain for user chords = **app ∪ plugins** (both matched, always co-active), plus modal "toggle-close" chords that mirror their app opener (worktrees `Mod+E` :168, settings `Mod+Shift?+,` :158, quick-idea/idea-inbox `Mod+I`/`Mod+Shift+I` :176-196, help `HELP_CHORD` :88,203,215).
- `FE/app/keyboard.ts:29-92` — capture-phase dispatcher; first match wins within a scope (`matchBinding` `.find`, key-scopes.ts:441-453). **No dispatcher change needed** for runtime bindings.
- `FE/app/command-registry.ts` — `registerCommandSource(fn,'core'|'plugins')`, re-read on every lookup; `Command {id, title?, shortcut?, run()}`.
- `FE/app/commands.ts:329-379` — `MENU_COMMANDS` maps `menu:<id>` → command id. Palette ids drift from binding ids (`move-forward` vs `arrow-shift-up`; `reopen-closed-session`, `grid-left/right` have no palette label).

**Display surfaces (criterion 2) — none derived from binding data today**
- `FE/lib/shortcuts.ts:1-349` — hand-written `shortcutGroups({isMac})` (7 groups, `{keys,label}`, no command ids, several merged rows: ⌘1–9, arrows, `⌘[ / ⌘]`, zoom, `⌘? or ⌘/`) and `paletteShortcuts({isMac})` (id → label). Glyph helpers `mod()`/`ctrl()`/`ctrlAlt()` + `KEYS` table.
- Help overlay `FE/components/modals/HelpOverlay.tsx:37-50` — recomputes per open (`useMemo` on `entry.seq`), plus a Plugins group.
- Palette `FE/app/modals/command-palette.ts:34` — `CORE_KEYS = paletteShortcuts(...)` **computed once at module load** (stale after rebind).
- Hard-coded rebindable-command hints (drift risk): `FE/../index.html:98` (`New project (⌘N)`, mac-only static), `FE/lib/status.ts:94-115` `modeHints()`, `FE/lib/empty-state.ts:35-63` (⌘T/⌘N), `FE/app/events.ts:1234` ("Worktrees (⌘E)"), `FE/components/ProjectCard.tsx:134,160`, `FE/components/modals/IdeaInbox.tsx:86,92`, `FE/components/modals/Help.tsx:108`, `FE/components/SessionRow.tsx:170,337` + `Sidebar.tsx:97-107` (`[n]` digit hints imply switch-N).
- Exempt (overlay-local Enter/Esc/r/arrows, non-goal): TileOverlays, Settings, QuickIdea, Launcher, Worktrees `(r)`, LauncherAgents ⌥↑↓, ModalShell `[esc]` hints. `EditorSettings.tsx:23` ⌘-click is a mouse modifier, out of scope.
- UI is React 19 + zustand 5 (`FE/store/store.ts:532`, `useAppStore` :1393). No bindings in the store today.

**macOS native menu**
- `cmd/hivegui/menu_darwin.go:22-204` `buildAppMenu` — accelerators hard-coded via `keys.CmdOrCtrl`/`keys.Combo`. New/Close Window call Go directly. `menu_other.go:21` returns nil (no menu off-mac).
- `cmd/hivegui/app.go:93-104` `SetDebugTrace` — precedent for runtime rebuild: `buildAppMenu` → `MenuSetApplicationMenu` + `MenuUpdateApplicationMenu`. Wails v2.15.0.
- AppKit consumes a registered key equivalent before the webview sees keydown. So: a rebound command with a menu item **must** have its Go accelerator changed (else old ⌘ chord still fires via menu); a menu item shows only ONE accelerator — extra chords and non-⌘ chords go through keydown; a ⌘ chord on a command without a menu item works via keydown unless some menu item still holds it. AppKit matches the unshifted char (`⌘/` also eats ⌘?, menu_darwin.go:188-201). find-in-session has no mac keydown chord (key-scopes.ts:321-327).
- Wails role-menu reserved keys (WailsMenu.m): ⌘H ⌥⌘H ⌘Q; ⌘Z ⇧⌘Z ⌘X ⌘C ⌘V ⌘A ⌥⇧⌘V; ⌘M ⌃⌘F. ⌘Z is claimed by both Edit→Undo and File→Reopen today.
- **Key capture in the webview cannot see menu-owned ⌘ chords** (settings.ts:57-64). Capture needs the menu accelerators suspended while a capture field is focused.

**Terminal-editing + hard-coded exceptions**
- `FE/app/session-term.ts:595-690` + `FE/lib/keymap.ts:67-122` — ⌘⌫, ⌘←/→ (`macLineEditSeq`), ⌥⌦/⌘⌦ (`macForwardKillSeq`), Shift+Enter (`isShiftEnter`), Ctrl+Shift+C/V/A. Predicates, not chord data — read-only group in the tab.
- Ctrl+C has no handler (reaches xterm because nothing claims it). Off-mac `Mod+C` = Ctrl+C → must be refused.
- Non-editable command ids: `*.close`, `*.dismiss`, `dead-session.*`, `session.answer-worktree-question`, `inline-rename.cancel`, and `command: null` reservations.

**Plugins**
- `FE/lib/plugin-api.ts:14-147` — `PluginKeys {key:[A-Za-z0-9], shift?}` → `pluginChord` = `Mod+[Shift+][KeyX|DigitN]`. `resolveCommands` refuses a plugin chord whose **display label** is in a core label set; loser gets `bound:false` + `console.warn` only (silent to user).
- `FE/app/plugin-host.ts:482-552` — `coreLabels` memoized forever; ids `plugin:<pluginId>:<commandId>`; `pluginCommands()` memoized on `pluginUI`.

**Settings + persistence**
- `FE/components/modals/Settings.tsx` (1320 lines) — `TabId` :105, `tabsFor()` :109, `<Tabs>` :781, `<Panel>` :1260; draft + single `saveSettings()` (:666-714); root keydown Enter=save excludes plugins panel (:733-755) — a capture field must be excluded too. Panel precedent: `PluginsPanel.tsx`, `EditorSettings.tsx`.
- GUI-owned StateDir JSON precedent: `cmd/hivegui/editor_prefs.go:59-121` (missing → defaults, unparseable → error so save can't clobber, BOM strip, temp+rename). StateDir survives GUI reload, daemon restart, upgrade; Go can read it at startup to build the menu. **No wire/daemon change → no DaemonContract bump.**
- `DESIGN.md:99` lists GUI-owned StateDir files (already misses `editor.json`) — add `keymap.json`.
- No file dialogs today beyond `OpenDirectoryDialog` (`app_calls.go:487-511`); Wails `SaveFileDialog`/`OpenFileDialog` available.
- New Go binding = 4 places: `FE/bridge.ts` named re-export, `test/e2e/wails-mock.ts` (with `maybeFail`), `test/e2e-real/wails-bridge.ts`, DOM `vi.fn` mocks. `bridge-harness-parity.test.ts` enforces 2–3.

### Constraints / dependencies

- #478 shipped (PR #480) — the substrate exists; label derivation was explicitly deferred to #477 (478 plan :154, :333).
- macOS menu accelerators vs webview keydown (above) is the biggest technical risk: both key capture and rebinding menu commands depend on Go-side menu rebuild.
- No reserved-key lists exist as data; both OS-reserved and terminal-reserved lists are new.
- `keymap-parity.test.ts` pins pre-478 behaviour of defaults — must stay green (criterion 11).
- `menu_darwin_test.go` calls `buildAppMenu(&App{})` — the default path must keep its current accelerators.

### Prior lessons

- After a binding change, grep the whole tree for the key glyph and its word form — stale routing comments survive and mislead (`keybinding-change-stale-routing-comments`). `menu_darwin.go:17-18` already says shortcuts live in `main.ts` (stale).
- Read xterm's `evaluateKeyboardEvent` before deciding terminal-key behaviour — informs the terminal-reserved warning list.
- Mocking `platform.isMac` doesn't change `cmdOrCtrl`'s default arg; in DOM tests press chords with `ctrlKey` or mock `cmdOrCtrl`, and assert labels and presses separately.
- Fresh worktree: `./scripts/ci-bootstrap.sh`, then `npm install` + build in FE before `tsc`; new Go bindings need `wails generate module` (wailsjs is gitignored).
- Buttons disabled before an async Go call must re-enable on reject (Export/Import).

### Conventions card

- Build: `./build.sh` (macOS .app). Tests: `scripts/test.sh` (layers `go · unit · dom · e2e`); `npm run test:e2e:real` separate. Typecheck: `npm run typecheck` in FE (needs `./scripts/ci-bootstrap.sh`). Lint: `biome ci .` in FE, `scripts/ui-lint.sh`, `for os in darwin linux windows; do GOOS=$os go vet ./... ; GOOS=$os staticcheck ./...; done` under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- TDD: every behaviour change ships with its test; "boil the lake".
- Keybinding policy: chords in `key-scopes.ts` (`Mod`, never hard-coded modifiers), commands in `commands.ts` + `MENU_COMMANDS`, help overlay + palette, README Keybinds table (defaults only), changeset.
- UI: tokens only (`scripts/ui-lint.sh`), `Kbd` for every key hint, errors in the dialog error slot (no toasts), conflict state must not be colour-only.
- User-visible feature: `.changesets/<slug>.md` (`type: added`, `bump: minor`) + `site/features.json` entry with `since: "Unreleased"`. `DESIGN.md` update for the new persisted file. `CHANGELOG.md` is generated — never edit.
- Local Playwright with `CI=1`; validate layout in a real browser, not vitest.


## Approach

**Core idea:** the chord data from #478 becomes the single source for *dispatch, labels and the macOS menu*. A user keymap is a sparse override layer on top of the shipped defaults, per OS half, persisted as a GUI-owned `<StateDir>/keymap.json`. Everything that shows or fires a shortcut asks one resolver.

Obvious alternative rejected: keep `lib/shortcuts.ts`'s hand-written label tables and patch overrides into them. That keeps five drift surfaces and cannot satisfy criterion 2 ("old one appears nowhere") without a second hand-maintained mapping.

### Data model

- `keymap.json`: `{ "version": 1, "mac": { "<commandId>": ["Mod+Y", "Ctrl+Alt+K"] }, "other": { … } }`.
  - Key present → that list **replaces** the command's defaults on that OS. `[]` = unbound (palette/menu still run it). Absent = defaults.
  - Each string is ONE user shortcut (a single chord). A default binding entry may carry layout aliases (`['Ctrl+-','Ctrl+_','Ctrl+[Minus]']`, `HELP_CHORD`) — that whole entry is one shortcut; its label is its first alias.
  - Command ids cover core commands **and plugin commands** (`plugin:<pluginId>:<commandId>`); plugin defaults come from `pluginChord(keys)`. So plugins are rebindable and relabelled through the same resolver.
- **Effective bindings** (`effectiveBindings(defaults, overrides, isMac)`, pure):
  1. Overridden commands get their override chords, placed **first** in the list.
  2. A command **without** an override whose default shortcut overlaps any override chord **ships unbound entirely** (all of its default shortcuts, every alias) and is reported in `displaced` — spec criterion 8 verbatim ("ships that command unbound"). Phase 2 flags it in the tab.
  3. Invalid chord strings (parseChord throws) or unknown command ids are ignored and reported, never fatal.
- **Reserved chords** (`RESERVED_CHORDS`, data in `app/bindings.ts`): the `command: null` reservations (`Mod+Shift+Enter`, `Mod+Shift+Z`) plus the terminal-editing / clipboard chords handled as predicates in `session-term.ts` / `lib/keymap.ts` (off-mac `Ctrl+Shift+C/V/A`; mac `Mod+ArrowLeft/Right`, `Mod+Backspace`, `Alt+Delete`, `Mod+Delete`; `Shift+Enter` everywhere). Used by the plugin collision check now (keeps today's refusal of a plugin ⇧Ctrl+C off-mac) and as the "terminal-editing" refusal set in Phase 2.
- `chordsOverlap(a, b, isMac)` in `lib/chord.ts`: true when some key event could match both (same key after `[Code]`→key normalization; each modifier compatible, `?`/`Any` = wildcard). On mac, `?` overlaps `Shift+/` (AppKit matches accelerators on the unshifted character). Used for displacement, plugin collision, Phase 2 conflicts.
- Modal **mirror chords** (worktrees ⌘E closes worktrees, ⌘, closes settings, ⌘I/⇧⌘I idea toggles, help chord) are derived from the opener command's effective keys, so they follow a rebind.

### Labels

- New `lib/chord-label.ts` `chordLabel(chord, isMac)`: mac glyphs in Apple order `⌃⌥⇧⌘` + key glyph (`↩ ⌫ ⌦ ↑↓←→`, letters upper-case, `[KeyJ]`→`J`, `?` modifiers omitted); other platforms `Ctrl+Alt+Shift+Key` words. Must reproduce every label `shortcuts.ts` prints today, byte for byte.
- `shortcutLabel(commandId)` = labels of the command's effective shortcuts joined ` / `; `''` when unbound. Plugin palette labels (`plugin-api.ts` `chordLabel`) use it too.
- `lib/shortcuts.ts` stays pure but takes a resolver: rows become `{command, label}` (keys derived), `{keys, label}` (static non-command rows: double-click, ⌘-click, terminal editing, overlay keys), or **merged rows** `{commands:[…], keys:<today's compact text>, label}` (⌘1–9, arrows, `⌘[ / ⌘]`, zoom, `⌘? or ⌘/`). A merged row keeps its compact text while every member is at its default; once any member is overridden it expands into one row per member with that member's live label. `paletteShortcuts` becomes a derived lookup (no table). The same merged-row helper serves `lib/status.ts` `modeHints` (the `⌘↑↓←→` "move" hint spans four commands).
- Every hard-coded hint for a rebindable command reads `shortcutLabel` (list in Files to change). Deliberately **out of scope**: `EditorSettings.tsx:23-24` (⌘-click is a mouse modifier) and overlay-local keys (`LauncherAgents.tsx:147-148` ⌥↑↓, `[esc]`/`[enter]`/`(r)` hints) — non-goals.

### Reactivity and boot

- `app/keyboard.ts` installs its listener on import (`main.tsx:84`), before any `await`; there is no `initKeyboard`. So nothing assumes "loaded before dispatch": the store `keymap` slice starts empty (= defaults), `main.tsx` calls `GetKeymap()` asynchronously and `setKeymap` when it lands (failure → defaults + one `console.warn`).
- Every surface derives from the store and recomputes on a keymap **reference** change: dispatch (`app.bindings()` memoized on the keymap ref), palette rows (per call), help overlay (per open + subscribe), components (`useAppStore(s => s.keymap)`), and the three imperative ones via one `subscribeKeymap(fn)` helper that compares references: status-bar `setModeHint` recompute (main.tsx:149, view.ts:153/469 share a `refreshModeHint()`), the `#new-project-btn` title, and the native menu push.
- Boot gap (documented, accepted): until `GetKeymap` resolves (~ms), defaults fire — keydown and native menu alike.

### macOS native menu

- AppKit consumes a menu accelerator before the webview sees keydown, so the menu **must** carry the user's chord, not the default.
- Go keys the override map by **command id** (not menu event name — `menu:move-session-forward` → `move-forward`). `buildAppMenu` routes every item through `a.accel("<commandId>", <default>)`; `new-window`/`close-window` included.
- Frontend `menuAcceleratorOverrides(keymap)`: for each native-menu command, compare the accelerator derived from **defaults** vs **effective** bindings; send only differing items. `toMenuAccelerator(chord)`: first shortcut of the command containing `Mod`, mapped to Wails `keys.Parse` syntax — `Mod`→`cmdorctrl`, `Ctrl`→`ctrl`, `Alt`→`optionoralt`, `Shift`→`shift`, `?`-modifiers dropped; keys `ArrowUp/Down/Left/Right`→`up/down/left/right`, `Backspace`→`backspace`, `Enter`→`return`, `Delete`→`delete`, `Escape`→`escape`, `+`→`plus`, `[KeyJ]`→`j`, `[DigitN]`→`n`, `[Minus]`→`-`, `[Equal]`→`=`, `[Backquote]`→`` ` ``, `[BracketLeft/Right]`→`[`/`]`, `[Comma]`→`,`, `[Slash]`→`/`; letters lower-case. `""` = no accelerator (unbound, or only non-⌘ chords, which stay on the keydown path).
- **Parity fixture** `cmd/hivegui/testdata/menu-default-accelerators.json` (`{commandId: accel}`): a TS test asserts `defaultMenuAccelerators()` equals it, and a Go test asserts the map `buildAppMenu(&App{})` records equals it: `a.accel(commandId, def)` writes `commandId → resolved accel` into a per-build map (Wails `menu.MenuItem` has no id field), which also proves every item goes through `accel`. Items with no accelerator (`restart-session`, `reload-gui`, `restart-hive`, `check-for-updates`, debug items) are in the fixture as `""`. This ties the TS-derived defaults to Go's hard-coded ones so "send only differing items" is sound.
- Go `App.SetMenuAccelerators(map[string]string)`: copies the map, stores it under a `sync.Mutex` that also guards `debugTrace`, and builds the menu via `menuLocked()` (builds under the lock, ctx-independent; `buildAppMenu` never takes the lock — documented contract, so `buildAppMenu(&App{})` tests stay lock-free), then calls the Wails `MenuSet*` functions outside it (Wails runs bound calls on their own goroutines — `frontend.go:427` — so the `app.go:77-80` "main thread" comment is wrong and gets fixed). An accelerator that fails `keys.Parse` → **nil** (no accelerator) and a log line, never the default (keeping the default would leave the old ⌘ chord firing).
- Empty override map and nothing sent before → no call at all (criterion 11). A later reset-to-defaults sends `{}`.

### Persistence

- `cmd/hivegui/keymap_prefs.go` copies `editor_prefs.go`: `GetKeymap()` (missing → empty, BOM strip, unparseable → error so a later save can't clobber it). `SaveKeymap()` lands in Phase 2 with its first caller. GUI-owned StateDir file → survives GUI reload, daemon restart, upgrade; no wire change, **no DaemonContract bump**.

### Plugins

- `resolveCommands` stops comparing **display labels** against the forever-memoized `coreLabels`; it checks `chordsOverlap` against the effective core chords **plus `RESERVED_CHORDS`**, and `pluginCommands()` memoizes on `(pluginUI, keymap)` refs. Losers keep `bound:false` (Phase 2 shows them as conflicts — criterion 4).

### Behaviour deltas (Phase 1) — operator sign-off required at plan approval

| # | Platform | Today | After | Why |
|---|---|---|---|---|
| D1 | Windows/Linux | ⇧Ctrl↑/↓ in a **grid** moves spatially (identical to Ctrl↑/↓; `handleArrow` ignores shift in grid, `actions.ts:185-188`). In single view it reorders. | ⇧Ctrl↑/↓ runs `move-forward/backward` (reorder) in every view — what macOS already does, because the native menu's ⇧⌘↓ item takes the key there (`menu_darwin.go:146-151`). | One command per shortcut. Today the key, the menu and the palette label point at different commands (`arrow-shift-*` vs `move-*`), so a rebind could not move "the" shortcut. `arrow-shift-up/down` are deleted. |
| D2 | macOS | find-in-session has no keydown chord (menu ⌘F only). | Mac default gains `Mod+F` (keydown backup). No visible change: the menu still takes ⌘F first. | A ⌘ chord must exist in the data for the menu accelerator to be derived from it. |

Criterion 11 ("no change in behaviour") is met except D1, which is a deliberate, recorded exception if approved. `keymap-parity.test.ts` changes only the D1 rows and the mac ⌘F row (D2; legacy `findKey` returned false on mac), each with a comment.

## Phase breakdown

**Phase 1 (this PR) — engine, persistence, derived labels, menu.** No UI. Covers criterion 11 (bar D1), and the engine halves of 1, 2, 4, 6, 7, 8 (driven by a store / mocked `GetKeymap` override in tests).

**Phase 2 — Settings → Shortcuts tab.** `components/modals/ShortcutsPanel.tsx` (+ Settings `TabId`/`tabsFor`): rows grouped like the help overlay with a search box; one section per plugin (rebindable through the same overrides); read-only "Inside a terminal" group (criterion 10); press-to-capture field — while focused, Go `SuspendMenuAccelerators(true)` so ⌘ chords reach the webview, and the Settings Enter=save handler skips it; add/remove several shortcuts, reset one, reset all (criterion 6); conflict detection with `chordsOverlap` across app ∪ plugins (both rows highlighted with icon + "conflicts with X" text, Reassign / Cancel, Save disabled while any conflict remains — criteria 3, 4); refusal with explanation for OS-reserved chords (⌘Q ⌘H ⌥⌘H ⌘M ⌃⌘F ⌘X ⌘C ⌘V ⌘A ⌥⇧⌘V ⌘` ⌥⌘N ⌘⇥; off-mac Ctrl+C, Alt+F4, Alt+Tab; bare Esc/Enter/Tab) and for `RESERVED_CHORDS` (terminal editing); warn-but-allow for terminal-reserved chords (bare Ctrl+letter, Alt/⌥+letter) — criterion 5; displaced commands flagged "unbound: your shortcut for X now uses its new default key" (criterion 8); `SaveKeymap` on Settings Save → store → live dispatch + labels + menu (criteria 1, 2, 7). **Multi-window:** each window is its own process (`app_calls.go:592-603`), so every window re-reads `GetKeymap()` on window focus (cheap, file-backed) and calls `setKeymap` only when the canonical JSON differs — test: two identical re-reads → zero `SetMenuAccelerators` calls; a changed file → one. Reassign writes an explicit override for the losing command holding its remaining defaults ("the other command loses that shortcut", not the whole command). `SaveKeymap` round-trips `plugin:*` overrides for plugins that are not loaded. Changeset (`added`, minor), `site/features.json`, README mention of the tab (defaults table unchanged).

**Phase 3 — import/export.** Go `ExportKeymap()` (`SaveFileDialog`, writes the keymap file) and `PickKeymapFile()` (`OpenFileDialog`, returns its text). TS `previewImport(text, current)` → rows marked ok / unknown command (skipped) / OS-reserved or reserved (skipped) / conflict (resolver from Phase 2); nothing changes until Confirm, which writes into the Settings draft (criterion 9). Changeset.

## Files to change (Phase 1)

1. `FE/lib/chord.ts` — add `chordsOverlap`, `toMenuAccelerator`.
2. `FE/app/key-scopes.ts` — export `DEFAULT_APP_BINDINGS`; `app.bindings()` → effective (memoized on keymap ref); plugins scope → effective plugin bindings; modal mirror bindings derived from opener keys; D1, D2.
3. `FE/app/commands.ts` — delete `arrow-shift-up/down`; update the arrows comment.
4. `FE/store/store.ts` — `keymap` slice + `setKeymap`.
5. `FE/lib/shortcuts.ts` — resolver-driven rows, merged-row helper, derived `paletteShortcuts`; rewrite header (drift surfaces shrink to key-scopes + commands + README + `menu_darwin.go` defaults, guarded by the parity fixture).
6. `FE/app/modals/command-palette.ts` — labels per call (drop module-load `CORE_KEYS`).
7. `FE/components/modals/HelpOverlay.tsx` — pass resolver; subscribe to keymap.
8. `FE/lib/plugin-api.ts`, `FE/app/plugin-host.ts` — chord-based collision against effective core chords + `RESERVED_CHORDS`; plugin labels via `shortcutLabel`; remove `coreLabels` memo and its reset (`:604`); `pluginCommands()` memo keyed on keymap too.
9. Hints → `shortcutLabel`: `FE/../index.html:98` (drop static title; set + refreshed from JS), `FE/lib/status.ts:94-115` + `FE/main.tsx:149` + `FE/app/view.ts:153,469` (shared `refreshModeHint()`, also called on keymap change), `FE/lib/empty-state.ts:35-63`, `FE/app/events.ts:1234`, `FE/components/ProjectCard.tsx:134,160`, `FE/components/modals/IdeaInbox.tsx:86,92`, `FE/components/modals/Help.tsx:108`, `FE/components/Sidebar.tsx:97-107` + `SessionRow.tsx:170,337` (`[n]` only while switch-n has its default; else its live label; nothing if unbound).
10. `FE/main.tsx` — async keymap load; `subscribeKeymap` → `SetMenuAccelerators` (darwin only), mode hint, new-project title.
11. Bridge plumbing for `GetKeymap`, `SetMenuAccelerators`: `FE/bridge.ts`, `T/e2e/wails-mock.ts` (`maybeFail`; mock `GetKeymap` returns a seedable keymap), `T/e2e-real/wails-bridge.ts`, and every DOM test with an explicit `vi.mock('../../src/bridge.js', …)` factory (grep at implementation time).
12. `cmd/hivegui/app.go` — `menuMu sync.Mutex`, `menuAccel` field, `SetMenuAccelerators`, `SetDebugTrace` under the same lock; fix the main-thread comment.
13. `cmd/hivegui/menu_darwin.go` — `a.accel(commandId, default)` on every item; fix stale "main.ts" (:17-18) and "Hive has no Edit menu" (:56-59) comments.
14. Existing tests on old signatures: `T/unit/plugin-chords.test.ts:20-25,91`, `T/dom/help-overlay.test.tsx:69-71`, `T/unit/shortcuts.test.ts:109-165`, `T/dom/keymap-parity.test.ts` (D1 + D2 rows).
15. `DESIGN.md` — GUI-owned StateDir file list: add `keymap.json` (and the missing `editor.json`).
16. `AGENTS.md` — Keybindings Policy: labels/menu derive from key-scopes; `menu_darwin.go` carries defaults only, pinned by the parity fixture; drop the shortcuts.ts step.
17. `.changesets/477-shift-arrow-reorder.md` — `fixed`/patch for D1 (only user-visible effect of Phase 1).

## New files (Phase 1)

- `FE/lib/chord-label.ts` — `chordLabel`.
- `FE/app/bindings.ts` — `effectiveBindings`, `RESERVED_CHORDS`, `shortcutsFor`, `shortcutLabel`, `menuAcceleratorOverrides`, `defaultMenuAccelerators`, `subscribeKeymap`, keymap types.
- `cmd/hivegui/keymap_prefs.go` (+ `_test.go`) — `GetKeymap`.
- `cmd/hivegui/testdata/menu-default-accelerators.json` — TS/Go parity fixture.
- `T/unit/fixtures/shortcuts-v0.json` — today's `shortcutGroups`/`paletteShortcuts` output for mac and other, generated from the **pre-refactor** code and committed in its own commit before the refactor.

## Tests (Phase 1)

- `T/unit/chord-label.test.ts` — `labelsMatchLegacy`: for every command in the v0 fixture, `shortcutLabel` with an empty keymap equals it, both platforms, and the test fails if any palette command lacks a fixture entry (no vacuous pass); glyph cases (`[KeyJ]`, `?`, `++`, Backspace, arrows).
- `T/unit/chord.test.ts` — `chordsOverlap`: `Mod+Shift?+1`/`Mod+Shift+1` true; `Mod+J`/`Mod+[KeyJ]` true; `Mod+J`/`Mod+Shift+J` false; `Any+Escape`/`Mod+Escape` true; mac `Mod+?`/`Mod+/` true. `toMenuAccelerator` for Alt, every arrow, Backspace, Enter, `+`, each `[Code]` form, Ctrl-only → `""`.
- `T/unit/bindings.test.ts` — override replaces default; `[]` unbinds; two shortcuts both fire; override overlapping another command's default → **that command fully unbound** (all aliases) + listed in `displaced`, asserting the exact `displaced` set per platform (mac: override `Ctrl+[Minus]` → `{nav-back}`, nav-back loses `Ctrl+-`/`Ctrl+_` too; other: `Ctrl+[Minus]` → `{zoom-out}`); unknown `plugin:*` ids are ignored for resolution but preserved in the keymap object; invalid chord / unknown id ignored + reported; only the current OS half applies; plugin command override; `menuAcceleratorOverrides({})` is `{}`; new-session → `Mod+Y` gives `{"new-session":"cmdorctrl+y"}`; Ctrl-only override → `""`; displaced menu command → `""`; `defaultMenuAccelerators()` equals `cmd/hivegui/testdata/menu-default-accelerators.json`.
- `T/unit/shortcuts.test.ts` — `groupsMatchLegacyWithEmptyKeymap` (deep-equals v0, both OS: criterion 11); `mergedRowExpandsOnOverride` (rebind switch-3 → row splits, new label present, `⌘3` absent anywhere in the output).
- `T/dom/keymap-overrides.test.ts` — store override new-session→`Mod+Y`: `Mod+Y` runs new-session, `Mod+T` runs nothing; unbound command still runs via `runCommand`; worktrees rebound → new chord closes the worktrees modal, old doesn't; D1: `Mod+Shift+ArrowDown` in grid runs `move-forward`; plugin command rebound → new chord runs it.
- `T/dom/keymap-menu-sync.test.ts` — mocked `SetMenuAccelerators`: (a) never called with an empty keymap; (b) one call per keymap-reference change; (c) `{}` after reset following overrides; (d) no call on unrelated store updates (sessions/status).
- `T/dom/status-bar-hints.test.tsx` — change keymap → status bar text shows the new chord, old chord absent.
- `T/dom/command-palette.test.tsx`, `T/dom/help-overlay.test.tsx` — after a keymap change (no reload) the row shows the new label, old absent.
- `T/unit/empty-state.test.ts`, `T/dom/empty-state.test.tsx` — override reflected; defaults unchanged.
- `T/unit/plugin-chords.test.ts` — plugin chord colliding with a rebound core chord → `bound:false`; plugin chord on a default the user moved away → bound; off-mac plugin `{key:'c',shift:true}` → refused (RESERVED_CHORDS); plugin `{key:'z',shift:true}` → refused (null reservation).
- `T/dom/keymap-parity.test.ts` — D1 + D2 rows updated with comments.
- `T/unit/bridge-harness-parity.test.ts` — passes with the two new bindings.
- `T/e2e/keymap-override.spec.ts` — mock `GetKeymap` seeds `{new-session: ['Mod+Y']}` (boot path): help overlay shows ⌘Y not ⌘T; palette row shows ⌘Y; Mod+Y opens the launcher; Mod+T does not. Second case: live update via `window.__hive` seed after boot.
- Go `cmd/hivegui/keymap_prefs_test.go` — `TestGetKeymapMissingIsEmpty`, `TestGetKeymapReadsFile` (HIVE_STATE_DIR temp), `TestGetKeymapCorruptIsError`, `TestGetKeymapStripsBOM`.
- Go `cmd/hivegui/menu_darwin_test.go` — `TestMenuDefaultsMatchFixture` (every item's accelerator == fixture); `TestMenuAcceleratorOverride` (`new-session`→`cmdorctrl+y` shows Y; `""` → nil; others keep defaults; unparseable → nil); `TestFixtureAcceleratorsParse` (every fixture value round-trips `keys.Parse`); existing tests unchanged. `TestMenuLockedConcurrent` under `-race`: goroutines hammer the setters' state writes and `menuLocked()` concurrently (the real build path, not the ctx-nil early return).

## Verification

```bash
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) scripts/test.sh go unit dom e2e
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test -race ./cmd/hivegui/...
(cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)
scripts/ui-lint.sh
for os in darwin linux windows; do GOOS=$os go vet ./... && GOOS=$os staticcheck ./...; done
```

Would fail on a wrong implementation: the v0-fixture parity tests fail on any label drift; `keymap-overrides`/e2e fail if the old chord still fires or the old label still shows; `keymap-menu-sync` fails on a missing or over-eager subscription; the menu fixture tests fail if TS and Go defaults drift; `TestMenuAcceleratorOverride` fails if the menu keeps the default. Manual macOS row (`wails dev` + hand-edited `keymap.json`): ⌘Y in the File menu next to New Session, ⌘T does nothing.

## Open questions / risks

- CI: confirm the macOS leg runs `go test -race ./cmd/hivegui/...` (`ci.yml`); darwin-only menu tests only mean something there.

- **Boot gap:** defaults fire (keydown and menu) until `GetKeymap` resolves. Accepted; Go reading `keymap.json` itself would duplicate the chord grammar in Go.
- `chordsOverlap` normalizes `[Code]` with a US-layout table; on other layouts two chords could overlap undetected. Dispatch stays single-fire regardless (first match wins, overrides first).
- ⌘Z: Edit→Undo (Wails role menu) and File→Reopen both claim it today; unchanged.

## Second opinion

- **Round 1** — `revise`, confidence 7. 12 must-fix items, all applied: plugin regression (terminal + null-reserved chords now in `RESERVED_CHORDS`); full `toMenuAccelerator` mapping with nil (not default) on parse failure; Go data race (mutex); imperatively-pushed status hints and boot-order claims corrected (subscription-driven, no `initKeyboard`); menu-sync DOM test; criterion 8 now unbinds the whole command; D2 parity row; D1 sign-off made explicit; TS/Go menu-default fixture; Phase 2 gaps (plugin rebinding, multi-window, terminal chords); missed test/blast-radius files.
- **Round 2** — `revise`, confidence 8. Confirmed every round-1 item resolved. 3 new narrow must-fix items, applied without a third round (loop rule): parity fixture mechanism (`a.accel` records a per-build map; Wails menu items have no id), race test exercises the real `menuLocked()` build path, Phase 2 focus re-read compares structurally before `setKeymap`. Nice-to-haves folded in: exact per-platform `displaced` sets, Reassign keeps loser's remaining defaults, unknown `plugin:*` overrides preserved, CI macOS `-race` check.

## Decision log

- **2026-09-30** — Keymap is per-OS: `keymap.json` stores overrides as `{mac:{…}, other:{…}}`; a rebind on macOS touches only the `mac` half; export carries both halves, import applies the current OS's half. Why: operator decision; defaults are already modelled per OS (`{mac, other}` in `chord.ts`), so a portable single keymap could not express them as overrides.
- **2026-09-30** — Ship in 3 phased PRs: P1 engine + persistence + derived labels + menu rebuild (no UI, zero visible change); P2 Settings → Shortcuts tab (capture, conflicts, reserved lists, plugin conflicts, upgrade flagging); P3 import/export with preview. Why: operator decision; one ~30-file PR is too large for a converging review loop.
- **2026-10-01** — Behaviour delta D1 approved: off macOS, ⇧Ctrl↑/↓ runs move-forward/backward (reorder) in every view, matching macOS; `arrow-shift-up/down` deleted. Why: operator approval at the plan stop; one command per shortcut is a precondition for rebinding. Recorded exception to criterion 11.
- **2026-10-01** — Second-opinion round 2 must-fix items applied without a third reviewer round. Why: feature-loop caps the reviewer at two rounds.
- **2026-10-01** — Resolver split: pure data + resolver in `lib/bindings.ts` (so pure `lib/shortcuts.ts`, `lib/status.ts`, `lib/empty-state.ts` can derive labels), store glue in `app/bindings.ts`; `keymap-sync.ts` takes the menu command ids from `main.tsx` rather than importing `commands.ts`. Why: keeps lib/ pure and the sync module testable without the action graph.
- **2026-10-01** — Shifted characters overlap their unshifted key only when the chord leaves Shift unnamed. Why: `Ctrl+Shift+_` (nav-forward) must not be displaced by a user `Ctrl+[Minus]`.

## Progress

- **2026-09-30** — Exec plan created; research started.
- **2026-10-01** — Plan approved (chat fallback after the HTML page timed out). Phase 1 implementation starting.
- **2026-10-01** — Rebased onto #486 (e2e sweep of every shortcut). The sweep passes unchanged. Three older tests needed edits (keymap-parity D1/D2 cells, plugin-chords helper signature, shortcuts.test `restart-session`); operator approved each before editing.
- **2026-10-01** — Phase 1 implemented; PR #487 opened. All layers green.

## Open questions
