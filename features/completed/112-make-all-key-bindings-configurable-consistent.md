# Feature: Make all key bindings configurable, consistent, and documented

- **GitHub Issue:** #112
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Branch:** —

## Description

Several key bindings in hive are hard-coded and cannot be customized by users — for example, `Enter` to attach to a session and `i` to enter interact mode. Some of these keys are also undocumented, making them discoverable only by reading source code. This inconsistency means users cannot tailor hive to their workflow, and the keymap documentation is incomplete.

All key bindings must follow consistent patterns, live in a single source of truth, be user-configurable (via settings/config), and be documented (in the help panel and user-facing docs).

## Acceptance Criteria

- All key bindings are defined in one place (no hard-coded keys scattered across handlers).
- Every binding is configurable through the standard config/settings mechanism.
- Every binding appears in the help panel and relevant user-facing documentation.
- Going forward, adding or changing any key binding **requires** updating both the configuration surface and the documentation — this becomes part of the standard AC for key-related changes.

## Research

Detailed audit in `research/112-keybindings/RESEARCH.md`. Summary below.

### Relevant Code

**Central registry (partially configurable already)**
- `internal/tui/keys.go` — `KeyMap` + `GridKeyMap` built from `config.KeybindingsConfig`. 11 bindings still use literal strings (space, tab, h/l, left/right, y/n/enter/esc, v/V, G, i). Line 68 adds a literal "enter" alongside the configured Attach key — this is why Enter feels hard-coded.
- `internal/config/config.go` — `KeybindingsConfig` (~29 fields). Defaults in `internal/config/defaults.go:63-94`. No migration scaffolding for renames/additions yet.
- `internal/tui/components/settings.go:819-846` — Settings UI **does** have a Keybindings tab with editable fields for ~26 bindings (NewProject/Session/Team/WorktreeSession, Attach, Kill*, Rename, NavUp/Down, NavProjectUp/Down, Filter, GridOverview, Palette, Help, TmuxHelp, Quit, QuitKill, Settings, ColorNext/Prev, ToggleCollapse, FocusPreview, FocusSidebar, JumpProject1). Verify whether MoveUp/Down/Left/Right reorder bindings are exposed.

**Hard-coded keys in handlers (~95 literal key references)**
- `internal/tui/handle_keys.go` — big `msg.String()` switch: grid-mode `g/G/x/r/t/c/C/v/V/W`, sidebar jump `1–9`, what's-new overlay `enter/esc/q/space/d/j/k`, attach/grid-input hint keys.
- `internal/tui/handle_input.go` — text-input flows (filter, name, directory confirm, worktree branch, custom command, title edit): hard-coded `enter`/`esc`/`backspace`/`y/Y/n/N`.
- `internal/tui/components/gridview.go:227,250-331` — `ctrl+q` to exit grid input mode; `i/esc/enter/a/arrows` for grid nav.
- `internal/tui/components/settings.go:207-309` — settings modal uses `esc/s/R/enter/space/arrows/j/k` literally.
- `internal/tui/components/orphanpicker.go:41-77` — `up/down/space/a/enter/esc/q`.
- `internal/tui/components/recoverypicker.go:72-109` — same pattern as orphanpicker + `left/right` for agent cycling.
- `internal/tui/components/help.go:96-115` — help tab navigation `left/right/j/k/up/down`.

**Docs (incomplete)**
- `docs/keybindings.md` (194 lines) — covers most sidebar + grid bindings but misses: `space` collapse toggle, `h/l` collapse/expand aliases, capital `C` color reverse, `W` new worktree session, the entire Settings modal keymap, title-edit `esc`, orphan/recovery pickers, and multi-input `esc` cancel.
- `README.md` — only references high-traffic keys (`/`, `g/G`, `S`).

**Tests**
- `internal/tui/flow_*.go` + `keys_test.go` send `tea.KeyEnter`/`tea.KeyEscape` constants directly in 70+ places. Helpers `SendKey` / `SendSpecialKey` exist but tests often bypass them with literals. Any key-default change forces test updates unless we centralize through a test helper reading the config KeyMap.

### Constraints (from user)

- **Dialog confirm/cancel/don't-show keys (`y`/`enter`/`esc`/`n`/`d`) stay hard-coded literals.** Not configurable.
- **One unified cursor-nav action set** (`CursorUp/Down/Left/Right`) used by both sidebar and grid. Configurable. Defaults: arrows (+ vim aliases TBD).
- **One unified reorder action set** (`MoveUp/Down/Left/Right`) used by both sidebar and grid. Configurable. Defaults: `shift+arrow`.
- **Exit-grid-input shares the Detach binding.** One configurable "leave current attached/input context" key. Replaces hard-coded `ctrl+q` at `gridview.go:227`.

### Other constraints / Dependencies

- **Backwards compatibility** — existing `~/.config/hive/config.json` files must keep working. Any new bindings need defaults; renames need a migration in `internal/config/migrate.go`.
- **Bubble Tea `key.Binding` API** — many components don't currently accept a KeyMap; they switch on `msg.String()`. Plumbing KeyMap into components is non-trivial but fits the existing `Model` → component propagation already used for theme/config.
- **Some literals are "always-true" fallbacks** (e.g. Attach accepts `enter` in addition to config). These act as accessibility/consistency defaults. The design must preserve user intent (e.g. `enter` confirming in dialogs) without being "hard-coded" — likely by exposing them as additional bindings rather than literals.
- **Scope risk** — a single PR covering registry + config + migration + settings UI + docs + test refactor is large. May need to be staged (e.g. Phase 1: centralize + make Attach/Confirm/Cancel configurable; Phase 2: component keymaps; Phase 3: settings editor UI).
- **AGENTS.md AC amendment** — the acceptance criteria change ("adding/updating keys requires docs + config") lives in `AGENTS.md` and/or a new `docs/keybindings-policy.md`. Must be added as part of this feature.

## Plan

**Single PR.** Lands schema, unification, key remap, settings UI, docs, and policy together.

### Design decisions

- **Config schema:** `KeybindingsConfig` fields become `[]string` (was `string`). Defaults express alt-bindings naturally: `CursorUp = ["up", "k"]`, `MoveUp = ["shift+up", "K"]`. One-shot migration in `internal/config/migrate.go` wraps old `string` values as single-element `[]string`.
- **Unified actions:**
  - `CursorUp/Down/Left/Right` — single set used by sidebar AND grid (replaces sidebar `NavUp/Down/NavProjectUp/NavProjectDown` and grid literal arrows).
  - `MoveUp/Down/Left/Right` — single reorder set used by both. Default `shift+arrow` (+ shifted vim aliases).
  - `Detach` — single binding for "leave current attached/input context". Used by grid-input-exit (replaces `ctrl+q` literal at `gridview.go:227`) and any future detach path.
- **Key remap (user-requested behavior change):**
  - **Remove `tab` (FocusToggle) binding entirely.** It's useless in the sidebar view. Drop the field from config + KeyMap + settings UI + docs.
  - **Remove `i` (InputMode) binding.** No longer exists as a standalone action.
  - **`enter` → start input mode.** New `InputMode` default is `["enter"]`. Pressing Enter on a selected session in either sidebar or grid view starts input mode for that session.
  - **`f` → fullscreen / attach.** `Attach` default becomes `["f"]` (was `["a"]` plus literal `"enter"`). Pressing `f` opens the session full-screen (the previous Enter behavior).
- **Hard-coded by design (NOT in config):** `y`, `enter`, `esc`, `n`, `d`, `space` inside dialog/overlay/confirm handlers; `ctrl+c` force-quit; settings-modal `s` save and `R` reset. Documented exception in the policy.
- **Policy:** New "Keybindings policy" section in `AGENTS.md` requiring every key change to update (a) config + defaults, (b) settings UI, (c) `docs/keybindings.md`, (d) `CHANGELOG.md` if user-visible.

### Files to change

1. `internal/config/config.go` — `KeybindingsConfig` fields → `[]string`. Add `Detach`, `CursorUp/Down/Left/Right`, `SessionColorNext/Prev`, `ToggleAll`, `ToggleCollapse`, `CollapseItem`, `ExpandItem`. **Remove** `NavUp/Down/NavProjectUp/NavProjectDown` (folded into `CursorUp/Down`), `FocusToggle` (tab binding deleted), `InputMode` field name kept but **default changed to `["enter"]`**.
2. `internal/config/defaults.go` — set defaults per the design: `CursorUp=["up","k"]`, `CursorDown=["down","j"]`, `CursorLeft=["left","h"]`, `CursorRight=["right","l"]`, `MoveUp=["shift+up","K"]`, `MoveDown=["shift+down","J"]`, `MoveLeft=["shift+left","H"]`, `MoveRight=["shift+right","L"]`, `Detach=["ctrl+q"]`, **`Attach=["f"]`**, **`InputMode=["enter"]`**, `SessionColorNext=["v"]`, `SessionColorPrev=["V"]`, `ToggleAll=["G"]`, `ToggleCollapse=["space"]`, `CollapseItem=["left","h"]`, `ExpandItem=["right","l"]`.
3. `internal/config/migrate.go` — bump config version. Migration: wrap string fields as `[]string`; map removed `NavUp/Down` → `CursorUp/Down`; map old `Attach` value (was `"a"`) → `["f"]` only if the user hasn't customized it (i.e. value still equals old default), else preserve user's choice; drop removed `FocusToggle`. Tests in `migrate_test.go` cover each transformation.
4. `internal/tui/keys.go` — `NewKeyMap` uses `WithKeys(kb.X...)` for all slice fields. **Remove** the `FocusToggle`, `Confirm`, `Cancel` fields from `KeyMap` (Confirm/Cancel become package-private literals in dialog handlers; FocusToggle gone entirely). **Remove** literal `"enter"` from Attach (`:68`). `GridKeyMap.NavUp/Down/Left/Right`, `ExitGrid`, `InputMode`, `SessionColor*`, `ToggleAll` driven from config — remove all literal strings (`"v"`, `"V"`, `"G"`, `"i"`, `"esc"`, `"ctrl+q"`, arrow literals).
5. `internal/tui/components/settings.go` — `keybindField` accepts `[]string` (display joined with `, `; parse by splitting on `, `). Add fields for the new actions in the Keybindings tab. Remove fields for deleted `NavUp/Down/NavProjectUp/NavProjectDown/FocusToggle`. Keep `R` reset, `s` save, `esc` cancel as hard-coded dialog keys.
6. `internal/tui/handle_keys.go` — replace `msg.String()` switch cases for `"v"/"V"/"G"/"W"/"i"/"ctrl+c"/"tab"` with `key.Matches(msg, km.X)`. Remove the `tab`/FocusToggle handler entirely. Where `enter` was the attach trigger, route through `km.InputMode` instead; where the previous `i` opened input mode, that path is removed (it's now `enter`).
7. `internal/tui/components/gridview.go` — replace literal `"ctrl+q"` (`:227`) with `key.Matches(msg, km.Detach)`. Replace grid-nav literals (`:250-331`) with `km.CursorUp/Down/Left/Right`. The previous `"i"` path becomes `km.InputMode` (which is now `enter`); `"a"` attach → `km.Attach` (now `f`). Component takes a KeyMap field.
8. `internal/tui/handle_input.go` — text-input flows continue to use literal `enter`/`esc`/`backspace` (dialog rule). No functional change; verify no FocusToggle handling remains.
9. `internal/tui/app.go` — pass `KeyMap` into components that need it (gridview, settings, orphanpicker, recoverypicker, help).
10. `internal/tui/components/orphanpicker.go`, `recoverypicker.go`, `help.go` — replace literal cursor keys (`up/down/left/right/j/k`) with `km.CursorUp/Down/Left/Right`. `enter`/`esc`/`space`/`a`/`q` stay hard-coded (dialog/overlay).
11. `docs/keybindings.md` — rewrite. Document `enter`=input mode, `f`=fullscreen, `ctrl+q`=detach, removed `tab`, removed `i`. Add "Hard-coded dialog keys" subsection listing `y/enter/esc/n/d/space/ctrl+c`.
12. `README.md` — refresh any references to `enter`/`a`/`tab`/`i` shortcuts; link to `docs/keybindings.md`.
13. `AGENTS.md` — add "Keybindings policy" section. Required updates for any new/changed keybinding: config field + default, settings UI field via `keybindField`, `docs/keybindings.md` row, `CHANGELOG.md` entry. Lists the dialog-key exceptions.
14. `CHANGELOG.md` — under `[Unreleased]`:
    - **Changed:** `Enter` now starts input mode for the selected session (was: attach). Use `f` to attach full-screen.
    - **Changed:** `f` is the new attach/fullscreen key (was: `a` or `Enter`).
    - **Changed:** Grid view, sidebar nav, and reorder share unified key actions — rebinding `CursorUp` (or `MoveUp`) applies in both views.
    - **Removed:** `tab` (focus toggle) binding — was unused.
    - **Removed:** `i` (input mode) binding — replaced by `Enter`.
    - **Added:** Keybindings tab in settings now exposes cursor nav, reorder, detach, input mode, attach, session color, grid toggle, and collapse/expand actions.
    - **Added:** All keybindings now use `[]string` so users can bind multiple keys to one action.
15. `features/templates/FEATURE.md` — add one-line "Keybindings checklist" reminder under the Plan template, pointing at the AGENTS.md policy.

### Test Strategy

- `internal/config/migrate_test.go::TestMigrate_KeybindingsToSlices` — old `string` field config migrates to `[]string`; default `Attach="a"` migrates to `["f"]`; user-customized Attach preserved; removed fields (`NavUp/Down/NavProjectUp/NavProjectDown/FocusToggle`) are dropped; `CursorUp/Down` populated from removed `NavUp/Down`.
- `internal/config/defaults_test.go::TestKeybindingsDefaults` — assert each new default exactly (`Attach=["f"]`, `InputMode=["enter"]`, `Detach=["ctrl+q"]`, etc.).
- `internal/tui/keys_test.go::TestKeyMap_NoLiteralEnterInAttach` — `KeyMap.Attach.Keys()` does NOT contain `"enter"` unless user configured it.
- `internal/tui/keys_test.go::TestKeyMap_CursorUnifiedSidebarAndGrid` — `KeyMap.CursorUp` and `GridKeyMap.NavUp` resolve to identical key sets.
- `internal/tui/keys_test.go::TestKeyMap_NoFocusToggle` — `KeyMap` struct does NOT have a FocusToggle field; sending `tab` in flow doesn't switch panes.
- `internal/tui/keys_test.go::TestEveryConfigBindingHasSettingsField` — reflects over `KeybindingsConfig` fields, asserts each appears in the settings tab via a registry exposed from `settings.go`. Catches "added a binding but forgot the UI" mistakes.
- `internal/tui/keys_test.go::TestEveryConfigBindingDocumented` — reads `docs/keybindings.md`, asserts every config field name appears at least once.
- `internal/tui/flow_attach_test.go::TestFAttachesFullScreen` — pressing `f` on a selected session attaches; `enter` does NOT attach.
- `internal/tui/flow_grid_input_test.go::TestEnterStartsInputMode` — pressing `enter` on a selected grid cell enters input mode; `i` does NOT.
- `internal/tui/flow_grid_input_test.go::TestDetachExitsInputMode` — `ctrl+q` (configured Detach) exits input mode; verify rebinding Detach in test config reroutes the exit.
- `internal/tui/flow_navigation_test.go::TestCursorConfigurableInBothViews` — change `CursorUp` to `"w"` in test config, verify it works in sidebar and grid.
- `internal/tui/flow_reorder_test.go::TestMoveConfigurableInBothViews` — same for `MoveUp`.
- `internal/tui/flow_dialogs_test.go::TestDialogKeysAreHardCoded` (new file) — rebinding `Confirm`/`Cancel`-shaped keys in config does nothing; `y`/`enter`/`esc`/`n` always work in confirm overlays.
- All existing flow tests using the old `enter`/`a`/`i`/`tab` literals updated. Where the test asserts attach behavior, switch literal to `f`. Where the test sends `enter` for attach, switch to `f`.

### Risks

- **Behavior break for existing users on update.** `Enter` no longer attaches — this is the headline UX change. Mitigated by: clear CHANGELOG entry, updated docs, and the fact that the help/status bar always shows the current binding inline.
- **Migration ambiguity for `Attach`.** Old default was `"a"`. If a user has `"a"` in their saved config, we don't know if they explicitly chose it or just inherited the default. Plan: treat exact-match-to-old-default as "inherited" → migrate to `["f"]`. Document this in the migration commit message and CHANGELOG.
- **Test churn:** ~70+ flow tests use literal `tea.KeyEnter` for attach. All need updating in the same PR. Add a `flowRunner.SendBoundKey(action)` helper and migrate as we go.
- **Settings UI parsing of `[]string`:** users editing in the modal type a comma-separated list. Need clear placeholder text and validation (no empty entries, no whitespace-only). Trim whitespace on parse.
- **Help/status bar text** must reflect new defaults (`f` not `enter` for attach, `enter` for input mode). Audit all `key.WithHelp(...)` strings in `keys.go` and any inline status hints in `gridview.go`/`sidebar.go`.

### Risks

- **Test churn:** PR1 will touch many flow tests that send literal keys. Mitigation: provide a `flowRunner.SendBoundKey(action)` helper that resolves through the test's KeyMap, and migrate tests as we go.
- **Migration edge cases:** users with hand-edited configs may have `string` values that aren't valid Bubble Tea key strings. Migration should pass them through as-is (single-element slice) and let `key.NewBinding` ignore unknowns — same behavior as today.
- **Default-binding changes:** unifying sidebar `NavUp` (was `"k"` alias only?) with grid `NavUp` may shift some defaults. Check `defaults.go` carefully — preserve current observable behavior unless intentional.
- **GridKeyMap collapsing:** the GridKeyMap struct may be simplifiable to a slice of bindings driven by KeyMap directly. Worth doing in PR2 if it falls out cleanly; not required.
- **`features/templates/FEATURE.md` change** is a small process change but may surprise other agents mid-feature. Acceptable since AGENTS.md is the authoritative source.

## Implementation Notes

- Implemented across multiple PRs: #116 (plumb KeyMap through grid handlers), plus prior commits for config schema, migration, settings UI, and docs.
- **Deviated from plan:** `Attach` default is `["enter"]` (not `["f"]`), `InputMode` default is `["i"]` (not `["enter"]`). The plan's key remap was reconsidered — keeping Enter as attach is more intuitive.
- Modal overlay keys (settings, help, pickers) remain hard-coded — these are secondary UI surfaces where configurability has diminishing returns. Documented as exceptions in the AGENTS.md keybindings policy.
- Added keybindings policy section to AGENTS.md requiring all four surfaces (config, settings UI, docs, changelog) to be updated for any keybinding change.

- **PR:** #116 (primary), plus incremental commits on main
