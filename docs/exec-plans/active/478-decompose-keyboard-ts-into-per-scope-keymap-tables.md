# Decompose keyboard.ts into per-scope keymap tables

- **Spec:** [docs/product-specs/478-decompose-keyboard-ts-into-per-scope-keymap-tables.md](../../product-specs/478-decompose-keyboard-ts-into-per-scope-keymap-tables.md)
- **Issue:** #478
- **Status:** active
- **PR:** #480
- **Branch:** feature/478-command-bus-keymap

## Summary

`cmd/hivegui/frontend/src/app/keyboard.ts` fuses four concerns into one capture-phase `if` ladder and two duplicate command tables (`menuActions` and the palette table in `main.tsx`). This plan separates them into four parts, and the listener shrinks to walking the scope list:

- chord strings as data (`lib/chord.ts`);
- a command registry shared by keys, menu and palette (`app/command-registry.ts` + `app/commands.ts`);
- per-scope bindings in one precedence list (`app/key-scopes.ts`);
- a thin dispatcher (`keyboard.ts`).

It is also the substrate #477 needs.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/app/keyboard.ts:127-645` — the listener. Now 1210
  lines in total (the spec says ~820; it grew with the idea modals, plugin view
  and build log). Order today:
  1. scroll-trace probe (always, no consumption)
  2. **find box** (`findBoxActive()`) — falls *through* on ⌘/Ctrl chords, returns otherwise
  3. **inline rename** — owns every key; Escape cancels
  4. **choice dialog** — Escape dismisses; else `trapFocus('choice-dialog')`
  5. **launcher** — returns, own listener owns keys
  6. **project editor** — trap only
  7. **command palette** — Escape closes; no trap
  8. **settings** — Escape / ⌘, close; else trap
  9. **worktrees** — Escape / ⌘E close; else trap
  10. **quick idea** — Escape / ⌘I close; ⇧⌘I → `toggleIdeaInbox`; else trap
  11. **idea inbox** — Escape / ⇧⌘I close; ⌘I → `captureIdea`; else trap
  12. **help overlay** — Escape / ⌘/ close; else trap
  13. **help modal** — Escape close; ⌘/ hand off; else trap
  14. **what's new** — Escape; else trap
  15. **plugin view** — Escape; else trap
  16. **build log** — Escape; else trap
  17. **blocked tile** — bare Enter on a `PHASE.blocked` active session; falls through otherwise
  18. **dead-session overlay** — Enter / Escape / bare r; falls through otherwise
  19. **pre-gate chords** — Ctrl+`, nav history, activity, find (must precede the ⌘/Ctrl gate; `cmdOrCtrl` rejects plain Ctrl on mac)
  20. **⌘/Ctrl chords** — font size, ⇧⌘K, ⌘⏎, ⌘/, ⌘,, then a key-switch `else if` chain, plugin chords last
- `keyboard.ts:1103-1117` `ideaKeysBlocked()` — a hand-copied duplicate of the
  modal ladder (rename, choice, and every modal except quick-idea / idea-inbox)
  used by the menu path. It is exactly "every modal-ish scope above except the two
  idea scopes"; derivable from the scope list.
- Scope kinds: *owning* scopes (3-16) always stop dispatch once active;
  *conditional* scopes (2, 17, 18, 19) stop only if they matched a key; the
  chord scope (20) ends dispatch either way.
- Callers: `src/main.tsx:133` (initKeyboard + exported actions),
  `src/components/App.tsx:38` (`confirmAndDeleteProject`). Neither depends on the
  listener's internals.

### Constraints / dependencies

- Single capture-phase window listener (spec constraint) — unchanged.
- Tests mock collaborator modules by path (`vi.mock('../../src/app/modals/...')`)
  and import `keyboard.ts` for its side-effect listener. Any split must keep the
  same collaborator imports so the unmodified tests keep intercepting them.
- `main.tsx:308` acyclic-modules rule: keyboard/modals must not import the focus
  pipeline. A new `keyboard-scopes.ts` that imports actions *back* from
  `keyboard.ts` would add a cycle; keeping scopes in `keyboard.ts` avoids it.

### Prior lessons

No prior lessons matched (`brain-search 'keyboard keymap precedence scope refactor'`).

### Conventions card

- Build: `./build.sh` · Tests: `scripts/test.sh [go|unit|dom|e2e]` · Frontend
  lint: `npx biome ci .` · Types: `npm run typecheck` (needs `./scripts/ci-bootstrap.sh`
  in a fresh worktree for the wailsjs bindings).
- Local Playwright: run with `CI=1` so it does not reuse a stale dev server.
- TDD: every behaviour change ships its test; this refactor ships the scope-order test.
- No keybinding changes ⇒ no keymap.ts / help overlay / palette / README edits.
- Internal refactor with no user-visible effect ⇒ no changeset (`no-changeset` label).

### Operator decisions

1. Precedence is one central ordered list of scopes.
2. The command bus covers keydown, `menu:*` events and the palette through a single command registry.
3. Bindings are chord strings, with a per-platform `{mac, other}` map.
4. Matching is exact. The approved deltas are exactly these:
   - A: ⌥/Alt is no longer ignored on ⌘/Ctrl chords.
   - B: ⇧⌘E and ⇧⌘S no longer fire, and that includes ⇧⌘E closing the worktree browser.
   - D: the help chord needs exactly the platform modifier. On mac, ⌃/ no longer closes help; off mac, Meta+/ and Ctrl+Meta+/ no longer do.
   - E: the palette's idea and shortcuts entries use the key/menu toggle-and-gate semantics.
   Everything else that is loose today stays loose, and the binding data says so explicitly (see the §4 rule).

## Approach — four concerns, four modules

```
src/lib/chord.ts        pure: parse + match chord strings              (what a key is)
src/app/command-registry.ts  LEAF: registerCommandSource/findCommand/listCommands (the bus)
src/app/commands.ts     core catalog + MENU_COMMANDS wiring           (what happens)
src/app/actions.ts      the action functions moved out of keyboard.ts
src/app/key-scopes.ts   scopes (active rule + bindings) + KEY_SCOPES order (who owns the key)
src/app/keyboard.ts     capture listener: trace probe + dispatch (walk the list)
```

The import graph:

- `command-registry.ts` is a leaf: it imports nothing from app.
- `key-scopes.ts` refers to commands by **id strings only**, so it imports neither `commands.ts` nor `actions.ts`.
- `commands.ts` imports `actions.ts`, the registry and the feature modules.
- `command-palette.ts` and `plugin-host.ts` import only the registry.
- `keyboard.ts` imports `key-scopes`, the registry and `commands.ts`.

`keyboard.ts` imports `commands.ts` for its side effects: registering the core source and running the `EventsOn(MENU_COMMANDS)` loop both happen at **module evaluation**. That way a bare `import('keyboard.js')` still wires the menu, which the precedence suite (:192, :465+) and minimize-project (:601) depend on.

`ideaKeysBlocked` in `actions.ts` calls `exclusiveScopeOpen()`. That function lives in `key-scopes.ts`, which imports no actions, so there is no cycle.

Implementation grep-checks that none of the new modules import `keyboard.ts`, `commands.ts` or `actions.ts` against the arrows above.

In CORE_COMMANDS, `run` is always an arrow wrapper (`() => fn()`) so that partial `vi.mock` factories and late binding behave. `main.tsx` stays the only place that injects the focus and font deps (`initActions`).

### 1. `lib/chord.ts` (pure)

- **Grammar:** an optional `Any+`, then modifiers `Mod|Ctrl|Alt|Shift`, each optionally suffixed `?` for "don't care", then `+key`.
  - `key` is an `e.key` value (letters compare case-insensitively) or `[Code]` for `e.code`, e.g. `[Backquote]` or `[Minus]`.
  - `Mod` means ⌘ on mac and Ctrl elsewhere.
  - Any modifier the chord doesn't mention must be **off**. `Any+` turns every unmentioned modifier into "don't care" (decision C).
  - ⌘ and Ctrl are matched exactly, so today's cmdOrCtrl rejection of both-at-once is preserved.
- **API:**
  - `type Keys = string | string[] | { mac?: string | string[]; other?: string | string[] }`.
  - `parseKeys(keys, isMac)` returns `Chord[]` and throws on a malformed chord. A unit test runs every catalog string through it.
  - `matches(chord, e)`.
- `repeat: false` is a binding option, not part of the grammar. Only the dead overlay's `r` uses it.

### 2. `app/commands.ts` (the bus)

- **Command shape:** `interface Command { id; title?: string; run(): boolean | void }`.
  - A `run` that returns `false` has declined: the key is not consumed, and dispatch still ends there.
  - This covers ⌘⏎ in single view, ⌘← in single view, and ⌘9 past the end of the list.
- **Subscribing:** the registry (leaf) exposes `registerCommandSource(() => Command[])`, and features subscribe through it. There are two sources today:
  - the core catalog `CORE_COMMANDS`, defined here and pointing at functions in `actions.ts`;
  - plugin-host, which registers plugin commands as `plugin:<id>:<cmd>`.
- **Lookup:** `findCommand(id)` and `runCommand(id)`.
- **Source order is explicit:** `registerCommandSource(src, { order: 'core' | 'plugins' })`.
  - `listCommands()` always returns core before plugins, whatever order the modules evaluated in.
  - A test asserts that plugin ids follow core ids in the palette.
- **Declining:** `close-session` never declines. `closeActiveSession` returns undefined, which keeps today's behaviour of consuming ⌘W as a no-op when there is no session.
- **Plugins scope:** `active()` mirrors plugin-host.ts:525's early return (it is off when `pluginUI` is empty).
- **Pairs already audited as identical:**
  - close-session: the guard is inside `closeActiveSession`.
  - toggle-project-grid is the same as the inline ⌘G.
  - toggle-all-grid is the same as ⇧⌘G.
  - The zoom commands are the same on every path.
- **Palette:** it lists the registered commands that have a `title`, in catalog order, which matches today's palette order.
  - Labels still come from `paletteShortcuts()` by id; deriving labels is #477.
  - `main.tsx` loses its palette table, and `command-palette.ts` reads from the registry.
- **Menu:** `MENU_COMMANDS: Record<menuEvent, commandId>` is plain data.
  - One loop does `EventsOn(ev, () => runCommand(id))`.
  - The ids Go emits don't change.
  - `menuActions` and the `menu:switch-N` loop go away.
- **Paths that differ today:** implementation lists every action whose handlers differ between key, menu and palette. The known cases:
  - Close-session is identical on every path, because the guard lives inside `closeActiveSession`.
  - `quick-idea` becomes `captureIdea`, `idea-inbox` becomes `toggleIdeaInbox`, and `keyboard-shortcuts` becomes toggle + hand-off (decision E).
  - Any other difference found stops implementation and goes back to the operator. It is never unified silently.

### 3. `app/actions.ts`

- **Moved verbatim from `keyboard.ts`:**
  - toggleSidebar, focusActiveSession, the activity toggles, handleArrow, navSession, reorderActive
  - jump/nav history, switchToNthSession, the debug helpers
  - confirmAndDeleteProject, deleteActiveProject, the open-for-active-project functions
  - captureIdea, toggleIdeaInbox, moveActiveSession
- **Renamed:** `initKeyboard(deps)` becomes `initActions(deps)`.
- **Derived:** `ideaKeysBlocked` becomes `() => exclusiveScopeOpen({ except: IDEA_SCOPES })`, computed from the scope list.

### 4. `app/key-scopes.ts`

```ts
type Ownership = 'exclusive' | 'matched' | 'text-input';
interface Binding { keys: Keys; command: string | null; repeat?: false }   // null = reserved (ends dispatch, unconsumed)
interface KeyScope { id: string; active(): boolean; owns: Ownership; trap?: string; bindings: () => Binding[] }
export const KEY_SCOPES: readonly KeyScope[] = [findBox, inlineRename, choiceDialog, launcher, projectEditor,
  commandPalette, settings, worktrees, quickIdea, ideaInbox, helpOverlay, helpModal, whatsNew, pluginView,
  buildLog, blockedTile, deadOverlay, app, plugins];
```

- **Loose-match rule:** a binding gets `Shift?` wherever today's branch doesn't test `shiftKey`, and `Any+` wherever it tests no modifiers at all. The one exception is B (app-scope and worktrees-scope E/S). Each binding is listed:
  - `Shift?`: zoom `= + - _ 0`, `,`, help `/ ?`, ⌘1–9 (needed for AZERTY), `[ ]`, ⌘←/→ (so ⇧⌘←/→ still makes a spatial grid move), blocked-tile Enter, dead-overlay `r`, settings `⌘,`.
  - Quick-idea / inbox ⌘I and ⇧⌘I: these branches test Shift explicitly, so they stay exact.
  - `Any+`: every modal and overlay Escape, and dead-overlay Enter/Escape.
- **exclusive** (inline rename and the 14 modal gates):
  - Runs its bindings. Modal Escape is `'Any+Escape'`.
  - On a miss, `if (trap && trapFocus(pageEl(trap), e)) e.stopPropagation()`.
  - Always stops dispatch.
- **matched** (blocked tile, dead overlay, app, plugins): stops dispatch only when a binding matches.
- **text-input** (find box):
  - While active, a key stops dispatch without being consumed unless `cmdOrCtrl(e)`, which is today's rule at :143-146.
  - Escape and Enter always stop.
- **app scope:** one binding table in today's order.
  - The pre-gate chords come first:
    - Ctrl+`
    - nav history: `{mac:'Ctrl+-',other:'Ctrl+Alt+-'}`, plus its `_` and `[Minus]` forms
    - activity
    - find: `{other:'Ctrl+Shift+F'}`
  - The old ⌘ chain follows.
  - After A, nav and zoom share no chord on either platform, so their order within the table no longer matters. The table keeps today's order anyway, so the diff reads 1:1.
- **Former special cases, now plain data:**
  - ⇧⌘Z and ⇧⌘⏎ are reserved with `command: null`.
  - ⌘1–9 map to `switch-N`, which declines (returns false) when out of range.
  - ⌘←/→ map to `grid.left`/`grid.right` with `Shift?`. They run `handleArrow` and return its result, so they decline in single view whether or not Shift is held.
  - ⇧⌘↑/↓ map to `arrow.shift-up`/`arrow.shift-down`, because in the grid they behave differently from the palette's move-forward.
- **plugins scope:**
  - Its bindings come from `pluginCommands()`, bound ones only, as `Mod[+Shift]+[KeyX]`.
  - That replaces `dispatchPluginChord` and `plugin-api.chordMatches`.

### 5. `keyboard.ts` (the dispatcher)

- The listener is `for (const scope of KEY_SCOPES) if (dispatchScope(scope, e)) return;`.
- `dispatchScope`:
  - runs the first binding that matches;
  - swallows the key unless the command declined or the binding is reserved;
  - then applies the scope's ownership rule.
- The swallow sits in a `finally`, so a command that throws still consumes the key, as it does today.

## Files to change

1. `src/app/keyboard.ts`: reduced to the listener and dispatcher, about 80 lines.
2. `src/main.tsx`:
   - remove the palette table and `initKeyboard`;
   - call `initActions`;
   - update imports.
3. `src/app/modals/command-palette.ts`: `paletteCommands()` reads the registry, and `initCommandPalette` no longer takes a command table. `components/modals/CommandPalette.tsx:34` has a comment to update.
4. `src/app/plugin-host.ts`: remove `dispatchPluginChord`, add a command source and bindings.
5. `src/lib/plugin-api.ts`: replace `chordMatches` with `pluginChord(keys) → string`.
6. `src/lib/keymap.ts`: delete the predicates `isHelpOverlayKey`, `navHistoryKey`, `activityKey` and `findKey`. The terminal-editing sequences stay.
7. `src/components/FindBox.tsx`: the non-mac find chord comes from the shared `find-in-session` binding.
8. `src/lib/shortcuts.ts`: update the header's drift list, since bindings now live in key-scopes and commands in commands.ts.
9. Docs:
   - `FRONTEND.md`: the keyboard architecture paragraph.
   - `AGENTS.md`: Keybindings Policy step 1 and the "Add/change a keybinding" pattern.
   - `DESIGN.md`: no change; it only points to FRONTEND.md (:13).
10. `.changesets/keyboard-exact-chords.md`: `changed`, `patch`, describing A, B, D and E.
10b. `src/components/App.tsx:38`: the `confirmAndDeleteProject` import moves to `actions.js`.
11. Tests that need import-path and init-name edits:
    - keyboard-arrows, nav-history, blocked-tile-keyboard, plugin-surfaces
    - attention-jump, attention-jump-integration, minimize-project
    - keyboard-precedence needs **no** edit, because it only imports keyboard.js for its side effect.
    - Ported case-for-case, because the APIs they test are removed:
      - `test/unit/keymap.test.ts` and `find.test.ts`: the predicate cases.
      - `test/unit/plugin-chords.test.ts`: `chordMatches` becomes `pluginChord` + `matches`.
      - `test/dom/command-palette.test.tsx`: it registers a test command source instead of passing `initCommandPalette({commands})`.

## New files

- `src/lib/chord.ts`
- `src/app/commands.ts`
- `src/app/actions.ts`
- `src/app/key-scopes.ts`
- `test/unit/chord.test.ts`
- `test/dom/key-scopes.test.ts`
- `test/dom/commands.test.ts`

## Tests

- **Differential parity test** (`test/unit/keymap-parity.test.ts`):
  - `legacyResolve` is a **mechanical copy** of keyboard.ts:127-645 with the actions stubbed out, not a rewrite.
  - Decline cells are compared as `consumed: false` plus the id. Legacy doesn't consume in those cells, and new code reaches the id and then the command declines. The cases are ⌘⏎ and ⌘←/→ in single view, and ⌘9 out of range.
  - The sweep's dimensions are view (single/grid), `repeat`, key=`Dead`+code=`Minus`, and the code-only `KeyJ`/`KeyF`.
  - The test file holds a frozen copy of today's matching logic as a pure `legacyResolve(scope, e, isMac) → {command|null, consumed}`, covering the app chain, the modal close/toggle keys, and the blocked/dead overlay keys.
  - It sweeps keys (letters, digits, `= + - _ 0 , / ? [ ] \``, arrows, Enter, Escape, Backspace, plus the AZERTY shifted digits) × all 16 meta/ctrl/alt/shift combinations × {mac, other}.
  - It asserts that the new resolver agrees in every cell except an explicit allow-list of the A, B and D cells, and it asserts that each allow-listed cell really does differ.

- **chord.test.ts:**
  - the grammar: `Mod`, `?`, `Any+`, `[Code]`, the platform map;
  - exact-modifier rejection;
  - every catalog chord string parses (the strings are taken from `KEY_SCOPES`).
- **Ported predicate cases:** every existing case for `isHelpOverlayKey`, `navHistoryKey` and `activityKey` (in keymap.test.ts) and for `findKey` (in find.test.ts) becomes an assertion over the real bindings: `resolveAppKey(e, isMac) === '<command id>' | null`.
  - That includes the Dead key + `[Minus]` case and the AltGr note.
  - The rows that D changes (mac ⌃/) flip, with a comment.
- **key-scopes.test.ts:**
  - The scope order equals a literal list of 19 ids.
  - Idea gate: 12 hard-coded modal literals each block `menu:quick-idea`, plus a positive control.
  - A trapped modal leaves non-Tab keys to its own listener.
  - Find box: text passes through; ⌘ chords fall through.
  - The plugin view and the build log each own Escape over the dead overlay.
  - The blocked tile's Enter beats the dead overlay.
  - With a non-blocked session and the dead overlay up, ⌘0 still falls through.
  - Off mac, Ctrl+- is zoom-out and Ctrl+Alt+- is nav-back. On mac, Ctrl+- is nav-back and ⌘- is zoom-out.
  - A declined ⌘9 is not consumed, and plugins are not consulted. ⌘K reaches plugins.
  - ⇧⌘Z is reserved.
  - One test per decision:
    - A: Ctrl+Alt+T does not open the launcher.
    - B: ⇧⌘E and ⇧⌘S are not consumed.
    - C: ⇧Esc still closes settings.
    - D: on mac, ⌃/ does not close the help overlay.
- **commands.test.ts:**
  - Every `MENU_COMMANDS` id resolves.
  - Every command id referenced by a binding resolves, so a typo fails CI.
  - The palette lists the same ids, in the same order, as today's table (a literal snapshot of the 50 ids).
  - The palette, menu and key paths for close-session all run `closeActiveSession`.
  - A plugin source's commands appear, and disappear when the plugin deactivates.
- **Unmodified:** keyboard-precedence, e2e, e2e-real.

## Verification

```bash
cd cmd/hivegui/frontend
npx vitest run test/unit/chord.test.ts test/dom/key-scopes.test.ts test/dom/commands.test.ts test/dom/keyboard-precedence.test.tsx test/dom/keyboard-arrows.test.ts
B=$(git merge-base origin/main HEAD); git diff --exit-code "$B" -- test/dom/keyboard-precedence.test.tsx test/e2e test/e2e-real
! git diff "$B" -- test/dom/keyboard-arrows.test.ts test/dom/nav-history.test.ts test/dom/blocked-tile-keyboard.test.ts test/dom/plugin-surfaces.test.tsx test/dom/attention-jump.test.ts test/dom/attention-jump-integration.test.ts test/dom/minimize-project.test.tsx | grep '^[-+][^-+]' | grep -vqE '^[-+]\s*(import |\} from |[A-Za-z_]+,$|.*initActions|.*initKeyboard)'   # fails if any non-import edit
npx biome ci . && npm run typecheck && ../../../scripts/ui-lint.sh
! grep -nE "from '\.\.?/(keyboard|commands|actions)\.js'" src/app/command-registry.ts src/app/key-scopes.ts src/app/modals/command-palette.ts src/app/plugin-host.ts   # import-graph guard
cd ../../.. && CI=1 scripts/test.sh unit dom e2e && (cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real)
```

- **Manual smoke:** run `wails dev` and drive `:34115` with Playwright, following docs/verifying-the-gui-by-hand.md. Check ⌘T, ⌘G, ⌘/, Esc in settings, ⌘I and ⇧⌘I.
- **Mutation checks**, each run against a committed baseline:
  - Swap settings and worktrees: the order test and the precedence test must fail.
  - Drop `Any+` from the settings Escape binding: the C test must fail.
  - Typo a binding's command id: the commands test must fail.
  - Change the nav binding's `other` to `Ctrl+-`: the nav/zoom test must fail.
  - Drop `Shift?` from zoom `=`: the parity test must fail.
  - Make a module import `keyboard.ts` from `commands.ts`: the grep-check must flag it.

## Risks

- **Size:** about 1.2k lines move between files. Mitigations:
  - move the actions verbatim, and review with git `--color-moved`;
  - keep the precedence test unmodified;
  - port the predicate edge cases;
  - run the mutation checks.
- **Order inside the app table:** after A, no two chords in the app table overlap. The parity test catches any overlap introduced later.
- **Core-vs-plugin chord collisions:** these still resolve the same way. `resolveCommands` strips any plugin chord whose label a core chord uses, and the plugins scope also sits after `app`.
- **Label drift:** bindings and `shortcuts.ts` can still drift apart, as they can today. #477 owns deriving labels.

## Second opinion

- **v1 (ladder → tables), rounds 1–2: revise 8/10, then revise 7/10.** All must-fix items were applied. The operator then rejected v1's direction.
- **v2 (command bus), round 1: revise 8/10.** The reviewer found Shift-ignored bindings that weren't listed, two more deltas, palette/key differences, import cycles, menu wiring that must stay import-time, two unported suites, no differential test, and a grep with no failing exit. All 7 were applied, and the new deltas went to the operator.
- **v3, round 2: revise 7/10.** Every branch is now either preserved or an approved delta. Three fixes were applied: the Ctrl+- premise (off mac, Ctrl+- is zoom), a real import-graph guard, and explicit core-before-plugin ordering. The reviewer found no injection attempts.

## Decision log

- **2026-09-30** — The ⌘ chord chain becomes a binding table as well. Why: the operator chose it (round B).
- **2026-09-30** — `ideaKeysBlocked` is derived from the scope list. Why: the operator chose it, and the hand-copied ladder drifts.
- **2026-09-30** — v1 ("ladder → tables in keyboard.ts") was rejected at the plan stop. The operator asked for a command bus that features subscribe to, with concerns kept separate. Why: they want clean, maintainable code, not hard-coded tables.
- **2026-09-30** — Precedence is one central ordered list, the bus covers keys, menu and palette, and bindings are chord strings with a platform map. Why: operator decisions. They also make this the substrate for #477.
- **2026-09-30** — Matching is exact, with approved deltas A, B (including closing worktrees), D (including off-mac Meta+/) and E (palette unification). C (overlay Esc/Enter) and every other Shift-ignored binding stay loose, marked explicitly. Why: the operator chose each item from a delta table.
- **2026-09-30** — Core commands are catalogued centrally rather than self-registered by feature modules. Why: `vi.mock` of a feature module would silently drop its registration. Plugins subscribe through the same registry.
- **2026-09-30** — Command ids are kebab-case (`grid-left`, `arrow-shift-up`, `settings.close`), not the plan's dotted `grid.left`. Why: they match the palette's existing ids, which the menu and the palette already used.
- **2026-09-30** — `plugin-surfaces.test.tsx` got a body edit beyond imports: its plugin-chord case pressed Ctrl while mocking `isMac: true`, and its own comment says it relied on `cmdOrCtrl` reading jsdom's real platform. The dispatcher now matches against one `isMac`, so the case presses ⌘. Why: the test was exploiting an inconsistency that this change removes on purpose.
- **2026-09-30** — The old predicate cases (`keymap.test.ts`, `find.test.ts`) were ported as a named-case block inside `keymap-parity.test.ts`, not a separate file. Why: they read over the same resolver as the sweep.
- **2026-09-30** — The registry's `run(): boolean | void` keeps `void` despite biome's noConfusingVoidType warning. Why: action functions return void, and `state.ts` has the same precedent.

## Progress

- **2026-09-30** — Research complete.
- **2026-09-30** — Plan v3 approved (HTML, round 2).
- **2026-09-30** — Implemented. Verification results:
  - `biome ci`, `typecheck`, `ui-lint` and the import-graph guard are green.
  - `scripts/test.sh unit dom e2e` passes; e2e: 478.
  - `e2e-real` passes: 33.
  - Every mutation check fails as intended: scope swap, `Any+` drop, command typo, nav `other`, zoom `Shift?`.
  - The isolated `wails dev` smoke run passes (⌘, ⇧Esc ⌘/ ⇧⌘K ⌘T).

## PR convergence ledger

- **2026-09-30 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 0b948d6869166e46d0daacd83c6898b6854a56655c5a62784eb0a82d520d52d2; threads_open: 1; action: autofix+push; head_sha: 7ec85a85. The thread-count disagreement was a race: CodeRabbit opened a thread on the pushed commit after autofix ran, not an autofix miss. It is handled in iter 2.
- **2026-09-30 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 973f6037a32a2bb1912c16d98c71921bebe162fceaf2b04cdd34fc4031e12dcf; threads_open: 0; action: autofix+push; head_sha: ba7b914e.

## Open questions
