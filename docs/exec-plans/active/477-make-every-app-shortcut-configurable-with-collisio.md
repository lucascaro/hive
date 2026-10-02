# Make every app shortcut configurable, with collision resolution

- **Spec:** [docs/product-specs/477-make-every-app-shortcut-configurable-with-collisio.md](../../product-specs/477-make-every-app-shortcut-configurable-with-collisio.md)
- **Issue:** #477
- **Status:** active
- **PR:** #490
- **Branch:** feature/477-keymap-import-export
- **Phase:** 3 of 3

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

## Files to change (Phase 2)

1. Go: `cmd/hivegui/keymap_prefs.go` `SaveKeymap`; new `cmd/hivegui/state_file.go` `writeStateJSON` (atomic temp+rename, now shared with `saveEditorSettings`); `cmd/hivegui/app.go` + `menu_darwin.go` `SuspendMenuAccelerators` / `menuSuspended` (a new `SetMenuAccelerators` lifts it — a fresh page's first push).
2. Bridge: `FE/bridge.ts`, `T/e2e/wails-mock.ts` (SaveKeymap writes the seed), `T/e2e-real/wails-bridge.ts`, Settings DOM mocks.
3. `FE/lib/keymap-edit.ts` (new): `captureChord`, `OS_RESERVED`, `checkChord`, `withShortcuts`, `resetHalf`, `reassign`, `canonicalKeymap`/`sameKeymap`.
4. `FE/lib/bindings.ts`: an override chord naming a default spelling inherits that entry's layout aliases (only the named chord displaces); mac `RESERVED_CHORDS` gains `Ctrl+Shift+C/V/A` (session-term handles them on every platform).
5. `FE/lib/plugin-api.ts` `ResolvedCommand.refused`; `FE/app/plugin-host.ts` `pluginCommands(state, keymap)`; `appChords` removed.
6. `FE/app/keymap-sync.ts`: `setShortcutCapture` on the menu queue (re-suspends after a push), focus re-read with `sameKeymap`.
7. `FE/app/key-scopes.ts`: `shortcut-capture` scope above `settings`.
8. `FE/components/modals/ShortcutsPanel.tsx` (new), `Settings.tsx` (tab, load/save, Save blocked on a pending conflict), `ModalShell.tsx` (`data-own-keys`), `IconButton.tsx` (`disabled`), `FE/lib/shortcuts.ts` (`commandGroups`, `terminalShortcuts`), `src/theme/components/settings.css`.
9. Docs: changeset `477-shortcuts-settings-tab.md`, `site/features.json`, README Keybinds intro, AGENTS.md policy, `docs/design-docs/ui/README.md` decision row.

## Tests (Phase 2)

- `T/unit/keymap-edit.test.ts` — capture (⌘/⌃ tokens, ⌥ and non-Latin fall back to `[Code]`, AZERTY letter, modifier-only null, Windows key refused), refusals (OS list, terminal list incl. mac ⌃⇧C, no-modifier except F-keys), warnings, edits, canonical compare.
- `T/unit/bindings.test.ts` — layout spellings come with an overridden default and follow a moved key; no spurious displacement.
- `T/unit/plugin-chords.test.ts` — `refused` lists the chords a plugin did not get.
- `T/dom/settings-shortcuts.test.tsx` — groups + read-only terminal group; capture + save → store; suspend/restore; conflict blocks Save (button and Enter) until Reassign, holder keeps its other keys; Cancel; OS refusal + terminal warning; remove/reset/reset all; displaced flag; plugin conflict + give-to-plugin; failed load never saves; search.
- `T/dom/keymap-menu-sync.test.ts` — focus re-read: identical file → no store write, no menu call; changed → one; capture suspend ordering.
- `T/dom/key-scopes.test.ts` — capture button focused: ⌘, / ⌘T / Esc run nothing.
- `T/e2e/shortcuts-tab.spec.ts` — real keyboard pipeline: ⌘, captured as a conflict, ⌘Y saved, old key dead, palette shows new; Escape cancels capture not Settings.
- Go: `TestSaveKeymapRoundTrips`, `TestSuspendMenuAccelerators`.

## Phase 3 — import/export and phase-2 review notes

FE = `cmd/hivegui/frontend/src`, T = `cmd/hivegui/frontend/test`.

### Approach

**Export.** A new Export… button in the Shortcuts toolbar sends the **draft** (what the tab shows, saved or not) to Go `ExportKeymap(k Keymap) (bool, error)`. Go opens `SaveFileDialog` (default name `hive-keymap.json`, `*.json` filter) and writes the keymap with `version: 1` and both halves, through the same marshal format as keymap.json. Cancelling returns `false`, and the frontend shows nothing. An error goes to the dialog's error slot, and the button re-enables either way.

**Import.** A new Import… button calls Go `PickKeymapFile() (string, error)`, which opens `OpenFileDialog` (`*.json`), refuses a file over 1 MiB, strips a BOM and returns the file's text (`""` on cancel). The text goes to pure `lib/keymap-import.ts`:

1. `parseKeymapFile(text)`. Not JSON, not an object, `version` other than 1/absent, or a half that is not an object: an error message, and nothing else happens. Otherwise `keymapFromJSON` (below) gives a clean keymap plus a list of malformed entries.
2. `previewImport(file, isMac, catalog)` takes only the **current OS half** (decision log 2026-09-30) and returns rows `{command, title, chord?, status}`, where status is one of:
   - `ok`, or `warn` with `checkChord`'s reason (bound).
   - `unbound` (`[]`), applied.
   - `unknown-command`, `reserved` (`checkChord` refused: OS, terminal, no modifier), `invalid` (unparseable chord) and `malformed` (a `null` or non-list entry). All four are skipped and say why.
   - `conflict` with holders.

   It also returns `candidate: Record<id, string[]>`, the accepted chords per command. `catalog` = `{ title(id), defaults(id) }` for every core and loaded-plugin command. The panel builds it: core defaults via `shortcutsIn(EMPTY_KEYMAP, id)`, plugin defaults via the unmemoized resolver (below) on an empty keymap.
3. Conflicts are computed live from the candidate: a chord of command X conflicts when another command Y holds an overlapping chord, where Y's chords are `candidate[Y]` if Y is in the import and `defaults(Y)` otherwise. This is the same rule the tab uses at capture time. Each conflict row offers:
   - **Reassign**: each holder loses only that key, through the same rule as `reassign()`. A holder outside the import gets an explicit override of its remaining defaults.
   - **Skip**: X drops that chord.

   Pairwise conflicts between two imported rows clear together.
4. **Confirm** is disabled while any conflict remains. It writes `applyImport(draft, candidate, isMac)` into the Settings **draft**: the current OS half is **replaced** by the candidate, with existing `plugin:*` overrides for plugins that are not loaded preserved, and the other half untouched. Save then persists it as usual. **Cancel** discards the import. Nothing reaches the draft before Confirm (criterion 9).

The preview renders **inline in the Shortcuts tab**, in place of the command list, with a header and Confirm/Cancel. It is not a nested modal, so there is no new key scope and no every-shortcut fixture. While the preview is open, `onBlockedChange(true)` blocks Save, the same mechanism as a pending conflict, and the footer reason reads "Finish or cancel the shortcut import".

**Phase-2 review notes folded in:**
- (a) New `keymapFromJSON(raw: unknown): { keymap, malformed: string[] }` in `lib/keymap-edit.ts`. It drops a non-object half, a non-list entry (`null`) and non-string chords, and keeps the rest. `loadKeymap` (keymap-sync) and Settings' `GetKeymap` read through it, so one `null` costs that entry only. Today `canonicalKeymap` spreads `null`, throws, and the catch falls back to all defaults. Import uses it too.
- (b) Split `pluginCommands` in `app/plugin-host.ts` into an unmemoized `resolvePluginCommands(state, keymap, warn)` and the memoized live one. The tab calls the unmemoized one with a no-op `warn` inside its own `useMemo`, so it never evicts the live memo, and the clash warnings fire once per live state again.
- (c) While the keymap is loading or failed, the Shortcuts tab shows an inline hint above the list: "Loading your shortcuts…", or "Shortcuts can't be edited: keymap.json could not be read. Fix or move the file, then reopen Settings." The Editor section gets the same treatment for `editor.json`, since it has the same gap.
- (d) `T/dom/settings-shortcuts.test.tsx` `beforeEach` calls `resetKeymapSyncForTest()`.
- (e) Go tests for the `SaveKeymap` write-error paths (below).

**Rejected alternatives.**
- *Merge import* (imported overrides layered over the current ones): an exported keymap lists only overrides, so merging can't reproduce the exporter's keymap. Replace is "use this keymap".
- *Export the saved file rather than the draft*: the button sits in a tab whose rows are the draft, so exporting anything else would surprise.
- *Nested modal for the preview*: it needs a new key scope and fixture, and Settings is already a modal.

### Files to change

1. `cmd/hivegui/keymap_prefs.go`: `ExportKeymap`, `PickKeymapFile`, with the dialog injected (`exportKeymapWith(save func() (string, error), k)`, `pickKeymapFileWith(open func() (string, error))`, the `pickDirectoryWith` precedent) so they are testable without Wails.
2. `cmd/hivegui/state_file.go`: factor the marshal (`stateJSONBytes`) so export writes the same bytes as keymap.json.
3. Bridge: `FE/bridge.ts`; `T/e2e/wails-mock.ts` (`ExportKeymap` records its argument on `window.__hive_mock`; `PickKeymapFile` returns a seedable text; both `maybeFail`); `T/e2e-real/wails-bridge.ts` stubs; DOM `vi.mock` factories that list bridge functions (grep at implementation time).
4. `FE/lib/keymap-edit.ts`: `keymapFromJSON`.
5. `FE/app/keymap-sync.ts` `loadKeymap`, and `FE/components/modals/Settings.tsx` `GetKeymap`: read through `keymapFromJSON`, with a single `console.warn` naming malformed entries. Settings also gets the import-blocked footer reason, and the inline loading/failed hints for the Shortcuts and Editor sections (via props).
6. `FE/app/plugin-host.ts`: `resolvePluginCommands` split.
7. `FE/components/modals/ShortcutsPanel.tsx`: Export…/Import… buttons, the preview mode, the disabled-state hint, and `resolvePluginCommands`.
8. `FE/components/modals/EditorSettings.tsx`: the disabled-state hint.
9. `FE/theme/components/settings.css`: preview row styles (tokens only).
10. Docs: `.changesets/477-keymap-import-export.md` (`added`, minor); (a) gets no separate `fixed` entry since the Shortcuts tab is still unreleased; `site/features.json`: the "Your own keyboard shortcuts" blurb gains "export and import them".
    - README Keybinds intro (line 299): "…and export or import your keymap there".
11. Plan bookkeeping: Decision log, Progress.

### New files

- `FE/lib/keymap-import.ts`: `parseKeymapFile`, `previewImport`, `importConflicts`, `reassignInImport`, `skipInImport`, `applyImport`.

### Tests

- `T/unit/keymap-import.test.ts`:
  - `parseKeymapFile` rejects non-JSON, an array, `version: 2`, and a non-object half.
  - `previewImport` takes only the current half (a `mac` row is ignored off-mac); marks unknown command, reserved (`Mod+Q` mac, `Alt+F4` other), no-modifier, invalid chord and null entry as skipped with a reason; `warn` for `Ctrl+A`; `[]` gives `unbound`.
  - Conflicts:
    - Imported vs a default: new-session `Mod+N` conflicts with new-project; Reassign gives new-project an explicit override of its remaining defaults (empty here); Skip drops it.
    - Imported vs imported: both rows conflict, and one Reassign clears both.
    - No false conflict when the holder's default moved in the import itself.
  - `applyImport` replaces the current half, keeps the other half, and keeps unloaded `plugin:*` overrides.
  - An export → parse → preview round-trip of a keymap equals it (criterion 9, "export produces a file import accepts").
- `T/unit/keymap-edit.test.ts` `keymapFromJSON`: `null` entry dropped with the others kept; non-string chord dropped; non-object half dropped; a non-object root gives the empty keymap.
- `T/dom/keymap-menu-sync.test.ts`: `loadKeymap` with one `null` entry keeps the other override in the store (fails today: the store stays `{}`).
- `T/unit/plugin-chords.test.ts` or a DOM test: calling `resolvePluginCommands` does not evict `pluginCommands`' memo, so a following `pluginCommands()` returns the same array and warns no more.
- `T/dom/settings-shortcuts.test.tsx`, with `resetKeymapSyncForTest` in `beforeEach`:
  - Export sends the draft and re-enables after a rejection.
  - Import → preview rows with statuses; Confirm is disabled while a conflict remains; Reassign enables it; Confirm writes the draft and Save sends it.
  - Cancel leaves the draft untouched and `SaveKeymap` uncalled.
  - Save is blocked while the preview is open.
  - A parse error goes to `#settings-error`, with no preview and the draft unchanged.
  - Import cancelled (`""`) does nothing.
  - The disabled-state hint shows when `GetKeymap` rejects; the Editor hint shows when `GetEditorSettings` rejects.
- `T/e2e/shortcuts-tab.spec.ts`: one new case. Seed the `PickKeymapFile` text with a `new-session → Mod+Y` keymap plus one unknown command. The preview shows the unknown command skipped; Confirm, then Save; Mod+Y opens the launcher and Mod+T does not. Export records the saved keymap.
- Go `cmd/hivegui/keymap_prefs_test.go`:
  - `TestExportKeymapWritesFile` (bytes equal SaveKeymap's) and `TestExportKeymapCancelled`.
  - `TestPickKeymapFileReadsAndStripsBOM`, `TestPickKeymapFileTooLarge` and `TestPickKeymapFileCancelled`.
  - (e) `TestSaveKeymapStateDirUncreatable`: HIVE_STATE_DIR under a regular file, so MkdirAll fails. `TestSaveKeymapRenameFails`: `keymap.json` is a directory, so the rename fails. Both leave no temp file behind.
- `T/unit/bridge-harness-parity.test.ts` passes with the two new bindings.

#### Verification

```bash
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) scripts/test.sh go unit dom e2e
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test -race ./cmd/hivegui/...
(cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)
scripts/ui-lint.sh
for os in darwin linux windows; do GOOS=$os go vet ./... && GOOS=$os staticcheck ./...; done
```

What would fail on a wrong implementation:
- The `null`-entry sync test fails on today's code.
- The preview tests fail if a skipped row is applied or a conflict lets Confirm through.
- The Cancel test fails if the draft is touched before Confirm.
- The e2e test fails if the imported key doesn't fire or the old one still does.
- The Go error-path tests fail if a temp file leaks.

Manual check: `wails dev`, export, then import the file back. The real dialogs open, and the e2e-real stubs are not exercised.

#### Open questions / risks

- Wails `OpenFileDialog`/`SaveFileDialog` with a default filename on macOS: is the filter honoured? Cosmetic either way.
- Window focus returns after the dialog and `loadKeymap` re-reads. It touches only the store, never the Settings draft, so a preview is not disturbed.
- Spec Notes open question (one keymap per OS?) was answered in phase 1 (per-OS halves). An export carries both halves, and an import applies only the current one.

#### Revisions after second opinion (round 1)

1. **All spellings.** Conflicts are checked against every spelling of every default binding (`chordsFor(b.keys)` over `DEFAULT_APP_BINDINGS`, the same set `effectiveBindings` displaces on). `shortcutsIn` returns only the first spelling of each binding, so it is not used for this check. A unit test covers a clash with an alias spelling: zoom-in `Mod+Shift?++`, and a `[KeyX]` alias.
2. **Resolver agreement test.** For a fixed set of imports, when the preview reports no conflicts, `effectiveFor(applyImport(…)).displaced` is empty and no two commands share an overlapping chord. Each case is checked on both platforms.
3. **Reason-typed block.** `onBlockedChange` becomes `onBlockedChange(reason: null | 'conflict' | 'import')`. The Settings footer reads "Resolve the shortcut conflict to save." or "Finish or cancel the shortcut import to save.". The off-tab link opens the Shortcuts tab and focuses Reassign (conflict) or the preview's first unresolved control, else Confirm (import). A DOM test covers each wording and link.
4. **When Import and Export are disabled.** Both are disabled while the keymap is loading or failed to load, while a capture is open, and while a conflict is pending. Import is also disabled while a preview is open. DOM tests cover these.
5. **Unloaded plugins round-trip.** An imported `plugin:<id>:*` entry whose plugin is not loaded passes through without checks, shown as "kept — plugin not loaded". The plugin host resolves it, core wins, when the plugin loads. For the same id, the imported entry wins over the draft's (replace semantics), and the draft's unloaded-plugin entries that the import does not name are kept. A test covers it.
6. **Catalog = the tab's list.** The catalog is the ids the tab lists: `commandGroups` rows, plus titled core `listCommands()` with no row, plus loaded plugin commands. The panel builds it from the same memo it renders. A test covers an override for a titled command with no default (for example `restart-session`), which counts as known.
7. **Note (a) scope stated.** Go still refuses the whole file when an entry is a string or a half is not an object, which is the editor.json rule: a later save must not clobber a hand-edit. Go decodes `null` to an absent list, so `null` is what reaches the frontend. `keymapFromJSON` handles `null`, along with the shapes that can come from import, where the JS parser sees the raw file. The DOM regression test for Settings: `GetKeymap` resolving with one `null` entry leaves the tab enabled, and the other override shows.
8. **Nice-to-haves folded in:** Esc while a preview is open cancels the preview, not Settings (DOM test). Catalog and candidate lookups use `Map` and `Object.hasOwn` (a `__proto__` key test). Go export test: `[]` exports as `[]`. The stale changeset item was removed.

#### Revisions after second opinion (round 2), applied without a third round

9. **`reassign` drops whole spelling groups.** When any spelling of a holder's binding overlaps the chord, the holder loses that whole binding, not just the overlapping spelling. Fixed once in `keymap-edit.ts`, so the tab gets the fix too; it is a latent phase-2 bug. Two tests: a unit test, in which zoom-in loses `Mod+Shift?+=` when `Mod+Shift?++` is reassigned, and a tab DOM case.
10. **Agreement test scope.** The agreement test also checks the state after every Reassign and every Skip, including the alias case.
11. **Unloaded-plugin entries are still checked.** These entries still go through the parse check and `checkChord`. A refused or invalid key is skipped. Only the conflict check is skipped.
12. **Esc ownership and wording.** The preview container is `data-own-keys`, and Esc there cancels the preview. The DOM test sends Esc from the preview's Reassign button. Note 7's wording is corrected: Go keeps a `null` entry as a present key with a nil slice, and Wails sends it on as `null`.

#### Operator decisions at the plan stop

13. **The import mode is the user's choice, made at import time.** The preview opens with a required choice: **Replace my shortcuts** or **Add to my shortcuts**.
    - **Replace:** the file becomes the current half, keeping unloaded-plugin entries.
    - **Add:** the file's entries override the matching commands, and every other override stays.
    - Conflicts are computed against the result of the chosen mode: a holder's keys come from its defaults (Replace), or from its current draft keys (Add). The rows render only after a choice is made, and Confirm stays disabled until then. The choice can be changed, which re-runs the preview.

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
- **Phase 3, round 1:** `revise`, confidence 7. Seven must-fix items, all applied: overlap checked against every spelling of every default; a test that the preview agrees with the resolver; a reason-typed `onBlockedChange`; when Import and Export are disabled; unloaded-plugin entries round-trip; the catalog is the list the tab shows; note (a) scope stated.
- **Phase 3, round 2:** `revise`, confidence 7. Three must-fix items, applied without a third round: `reassign` drops whole spelling groups; the agreement test runs after every Reassign and Skip; unloaded-plugin entries still go through `checkChord`.

## Decision log

- **2026-09-30** — Keymap is per-OS: `keymap.json` stores overrides as `{mac:{…}, other:{…}}`; a rebind on macOS touches only the `mac` half; export carries both halves, import applies the current OS's half. Why: operator decision; defaults are already modelled per OS (`{mac, other}` in `chord.ts`), so a portable single keymap could not express them as overrides.
- **2026-09-30** — Ship in 3 phased PRs: P1 engine + persistence + derived labels + menu rebuild (no UI, zero visible change); P2 Settings → Shortcuts tab (capture, conflicts, reserved lists, plugin conflicts, upgrade flagging); P3 import/export with preview. Why: operator decision; one ~30-file PR is too large for a converging review loop.
- **2026-10-01** — Behaviour delta D1 approved: off macOS, ⇧Ctrl↑/↓ runs move-forward/backward (reorder) in every view, matching macOS; `arrow-shift-up/down` deleted. Why: operator approval at the plan stop; one command per shortcut is a precondition for rebinding. Recorded exception to criterion 11.
- **2026-10-01** — Second-opinion round 2 must-fix items applied without a third reviewer round. Why: feature-loop caps the reviewer at two rounds.
- **2026-10-01** — Resolver split: pure data + resolver in `lib/bindings.ts` (so pure `lib/shortcuts.ts`, `lib/status.ts`, `lib/empty-state.ts` can derive labels), store glue in `app/bindings.ts`; `keymap-sync.ts` takes the menu command ids from `main.tsx` rather than importing `commands.ts`. Why: keeps lib/ pure and the sync module testable without the action graph.
- **2026-10-01** — Shifted characters overlap their unshifted key only when the chord leaves Shift unnamed. Why: `Ctrl+Shift+_` (nav-forward) must not be displaced by a user `Ctrl+[Minus]`.
- **2026-10-01** — Phase 2 conflict model: a captured key another command holds is pending — both rows marked, Save (and Enter) blocked until Reassign (the holder loses only that key) or Cancel; one pending at a time. Why: operator choice at the phase-2 start; matches criterion 3.
- **2026-10-01** — Plugin keys core already holds show as conflicts with Give-to-plugin / Drop, and do not block Save. Why: operator choice; an installed plugin's clash must not stop unrelated edits.
- **2026-10-01** — The tab lists the help-overlay groups plus an "Other commands" group of titled core commands with no row (palette-only ones like Restart session). Why: operator choice; criterion 1 says any core command.
- **2026-10-01** — A captured key with no ⌘/Ctrl/Alt is refused, F-keys excepted. Why: operator choice; the app scope matches globally, so a bare key would stop the user typing it in a session.
- **2026-10-01** — An override chord that equals a spelling of a default shortcut inherits that entry's other spellings. Why: otherwise editing ⌘J's command drops `Mod+[KeyJ]`, breaking it on non-Latin layouts; keeps keymap.json a flat list of chords (no Go type change).
- **2026-10-01** — Menu suspension follows the panel's `capturing` state, not focus events, and runs on keymap-sync's queue; Go's `SetMenuAccelerators` lifts it and the frontend re-suspends after a push mid-capture. Why: removing a focused button fires no blur, and a reload mid-capture must not leave the menu stripped.
- **2026-10-01** — Review iter 1 escalations, all applied on operator instruction: (1) focus moves to the row's + after any capture ends, to Reassign while a conflict waits, to the search box after Reset all; a capture ended by focus leaving is never pulled back; (2) while a conflict blocks Save the footer says so on every tab, linking back to Reassign off the Shortcuts tab (text link, so Save never wraps — checked in a browser screenshot); (3) menu suspension is reconciled (wanted vs. Go-confirmed state) instead of toggled: a failed call leaves Go's state unknown and is retried 3× with backoff, then on window focus, capture changes or menu pushes. Why: the real stuck case was a failed restore leaving the menu stripped; a failed re-suspend also used to roll back the menu-update bookkeeping.

- **2026-10-01** — Phase 3: Export saves the tab's draft (what it shows, saved or not). Why: operator choice at the phase-3 plan stop.
- **2026-10-01** — Phase 3: the import asks, at import time, whether to replace the current shortcuts or add to them. Why: operator choice at the phase-3 plan stop. The reviewer's case for Replace (only Replace reproduces the exporter's keymap) still holds, but it is offered as an option, not the default.
- **2026-10-01** — Phase 3 second opinion: round 1 `revise` (7, 7 must-fix), round 2 `revise` (7, 3 must-fix). All were applied without a third round, per the feature-loop cap.
- **2026-10-01** — Phase 3: `reassign` is unchanged. Round-2 must-fix 9 said the tab's `reassign` misses a clash on a second layout spelling. It does not reproduce: nine cases on both platforms (zoom-in `+`/`=`, nav-back `_`/`[Minus]`, `[KeyJ]` aliases, zoom-out off macOS) all pass on the phase-2 code, because every spelling of a binding overlaps its first. They stay in `keymap-edit.test.ts` as regression tests. Why: no patch without a reproducer.
- **2026-10-01** — Phase 3: Escape inside the import preview reaches it through the `shortcut-capture` scope, widened from the capture button's class to any `[data-own-keys]` element inside `#settings`. Without the widening, keyboard.ts's capture-phase `settings` scope would close Settings first. This adds no new key scope, so no every-shortcut fixture. The trade-off: ⌘, does not close Settings while focus is in the preview.
- **2026-10-01** — Phase 3: the footer reason for an open import is "Finish the shortcut import to save." The longer "Finish or cancel…" wrapped Save onto its own line (seen in a browser screenshot). The e2e test now asserts that Cancel and Save share a line.
## Progress

- **2026-09-30** — Exec plan created; research started.
- **2026-10-01** — Plan approved (chat fallback after the HTML page timed out). Phase 1 implementation starting.
- **2026-10-01** — Rebased onto #486 (e2e sweep of every shortcut). The sweep passes unchanged. Three older tests needed edits (keymap-parity D1/D2 cells, plugin-chords helper signature, shortcuts.test `restart-session`); operator approved each before editing.
- **2026-10-01** — Phase 1 implemented; PR #487 opened. All layers green.
- **2026-10-01** — Phase 2 started (reset: stage IMPLEMENT, Phase 2 of 3). Implemented on `feature/477-shortcuts-tab`.
- **2026-10-01** — Phase 2 merged (#488). Phase 3 reset: stage IMPLEMENT, Phase 3 of 3. Plan approved (chat). The flaky `plugin-view` e2e cell was filed as #489. Implementing on `feature/477-keymap-import-export`.
- **2026-10-01** — Phase 3 implemented: Go `ExportKeymap`/`PickKeymapFile` (+ `writeJSONFile`), `lib/keymap-import.ts`, `keymapFromJSON` at both keymap readers, the tab's import preview and Export…, reason-typed Save block, inline disabled hints (Shortcuts, Editor), `resolvePluginCommands`. Notes a–e done.

## Open questions

## PR convergence ledger

- **2026-10-01 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 0276aff780a0b7807f05c749e0716c603c3d57f6d088c91ba0a8a7fbac1589dd; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: d4429f7.
- **2026-10-01 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: c3055c8.
- **2026-10-01 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 3775bc2.
- **2026-10-01 iter 4** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: b7a97a1.
- **2026-10-01 iter 5** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: d24b2c0.
- **2026-10-01 iter 6** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 7dd95b51c1c6c61add98e00becbbf0ae3fdfa0dc0f7f88730742afcae0c6c183; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 5a8cd2d (phase 2, PR #488).
- **2026-10-01 iter 7** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 9e27c55 (phase 2, PR #488).
- **2026-10-01 iter 8** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: c4305d1 (phase 2, PR #488).
- **2026-10-01 iter 9** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 1050eddb7d215e556123e6bbc78f64ec8777620ebce9c6e53b82be5759d21864; threads_open: 1; action: escalated:required-ci-check-failed; head_sha: 0d95860 (phase 3, PR #490).
- **2026-10-02 iter 10** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 4f1a180510a4a8c34649afcc6b16fa98f036fb4b12a9d7aabb6ef9802185e14f; threads_open: 0; action: autofix+push; head_sha: 2b82b0a (phase 3, PR #490).
- **2026-10-02 iter 11** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 0b817d7 (phase 3, PR #490).

## Gate verdict

- **2026-10-01** — verdict: PASS; phase: 1/3; checks: 1 passed / 0 failed / 0 followups / 10 deferred; followups: none; one-line: Phase 1 engine, derived labels and menu sync verified; criteria 1–10 deferred to phases 2–3 (their Phase 1 engine halves verified), criterion 11 passes with the approved D1 exception.
  - 2026-10-01 dimensions:
    - acceptance — PASS — criterion 11 PASS (label + menu fixtures with empty keymap); 1–10 DEFERRED (phase > 1), engine halves of 1/2/4/6/7/8 exercised by bindings, keymap-surfaces, keymap-menu-sync, plugin-keymap, keymap-override e2e and Go keymap/menu tests
    - non-goals — PASS — terminal-editing chords only as RESERVED_CHORDS; modal Escape hard-coded; no sequences, sync, per-project keymaps; README untouched; no wire/daemon change
    - doc accuracy — PASS — changeset valid (type: changed); AGENTS.md policy and DESIGN.md StateDir list updated; no generated files edited
- **2026-10-01** — verdict: PASS; phase: 2/3; checks: 10 passed / 0 failed / 0 followups / 1 deferred; followups: none; one-line: Settings → Shortcuts tab delivers criteria 1–8 and 10 (capture, conflicts that block Save, plugin clashes, refusals/warnings, displaced defaults, read-only terminal keys), criterion 11 still holds for an empty keymap; 9 (import/export) deferred to phase 3.
  - 2026-10-01 dimensions:
    - acceptance — PASS — 1–8, 10, 11 PASS (settings-shortcuts, keymap-menu-sync, keymap-edit, bindings, key-scopes DOM/unit tests; shortcuts-tab e2e; Go TestSaveKeymapRoundTrips/TestSuspendMenuAccelerators); 7 persistence proven via the state-dir file, reload/upgrade not run end to end; 9 DEFERRED (phase 3)
    - non-goals — PASS — terminal keys and Ctrl+C refused, overlay keys not offered, no sequences/sync/per-project keymaps, 478 refactor untouched, README table unchanged, no wire/daemon change, no import/export leak
    - doc accuracy — PASS — changeset (added/minor) matches code, features.json since Unreleased (whats-new test passes), README pointer, AGENTS.md policy and UI decision row accurate, no generated files edited, DESIGN.md needs no change
