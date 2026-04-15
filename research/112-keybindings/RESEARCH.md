# Research — #112 Make all key bindings configurable, consistent, and documented

## Current architecture

Central registry lives in `internal/tui/keys.go`. `KeyMap` holds 31 `key.Binding`s for sidebar flow; `GridKeyMap` holds a grid-mode subset. Both are built from `config.KeybindingsConfig`.

`KeybindingsConfig` (internal/config/config.go lines 85-117) exposes ~29 string fields. Defaults live in `internal/config/defaults.go:63-94`. Loading/atomic saving is already implemented; no key-rename migration scaffolding exists yet.

## Hard-coded literals in keys.go

1. `keys.go:68` — `Attach: WithKeys(kb.Attach, "enter")` — `"enter"` appended to configured Attach. This is the biggest user-visible issue (Enter always attaches).
2. `keys.go:69` — `ToggleCollapse: " "` (space)
3. `keys.go:70` — `CollapseItem: "left", "h"`
4. `keys.go:71` — `ExpandItem: "right", "l"`
5. `keys.go:72` — `FocusToggle: "tab"`
6. `keys.go:92` — `Confirm: "y", "enter"`
7. `keys.go:93` — `Cancel: "esc", "n"`

GridKeyMap:
8. `keys.go:158-161` — NavUp/Down/Left/Right hard-coded to arrows
9. `keys.go:169-170` — `SessionColorNext "v"`, `SessionColorPrev "V"`
10. `keys.go:171` — `ExitGrid "esc"`
11. `keys.go:172` — `ToggleAll "G"`
12. `keys.go:173` — `InputMode "i"`  ← explicitly mentioned by user in AC

## Hard-coded literals in handlers

`internal/tui/handle_keys.go`
- `:19` — `"ctrl+c"` force quit
- `:31` — `"esc"` help close
- `:141-224` — grid mode switch on `"g", "G", "x", "r", "t", "c", "C", "v", "V", "W"`
- `:328` — `"G"` open all-grid from sidebar
- `:416` — `"v", "V"` cycle session color
- `:539-551` — `"1"`–`"9"` jump-to-project
- `:784-815` — what's-new overlay keys
- `:818-844` — attach hint keys

`internal/tui/handle_input.go` — `enter`/`esc`/`backspace`/`y,Y,n,N` across filter, name, directory confirm, worktree branch, custom command, title edit (lines 35–205).

`internal/tui/components/gridview.go`
- `:227` — `"ctrl+q"` exit grid input mode
- `:250-331` — `"i"`, `"esc"`, `"enter", "a"`, `"left"`, `"right"`, `"up"`, `"down"`

`internal/tui/components/settings.go:207-309` — settings modal uses `esc/s/R/enter/space/arrows/j/k`.

`internal/tui/components/orphanpicker.go:41-77` — `up/down/space/a/enter/esc/q`.

`internal/tui/components/recoverypicker.go:72-109` — same + `left/right`.

`internal/tui/components/help.go:96-115` — tab nav `left/right/j/k/up/down`.

## Configuration surface

`components/settings.go:819-846` builds a fully-populated Keybindings tab with editable per-binding fields for ~26 actions. `R` resets the whole tab to defaults. So the gap is NOT "no UI" — the gap is that several bindings exist as hard-coded literals in `keys.go` and handlers and never reach `KeybindingsConfig`, so they can't appear in the UI.

Bindings missing from the UI (because missing from config):
- Literal `"enter"` appended to Attach at `keys.go:68`
- `tab` (FocusToggle), `h`/`l` (CollapseItem/ExpandItem aliases), `space` (ToggleCollapse alt)
- `y`/`enter`/`esc`/`n` (Confirm/Cancel)
- `i` (InputMode), `v`/`V` (SessionColorNext/Prev), `G` (ToggleAll grid)
- `ctrl+q` (exit grid input), `ctrl+c` (force quit)
- All keys local to `orphanpicker`, `recoverypicker`, `help` tab nav, and the settings modal's own navigation
- Verify MoveUp/Down/Left/Right reorder bindings are exposed (config fields exist).

## Documentation gaps

`docs/keybindings.md` misses:
- `space` collapse toggle
- `h`/`l` collapse/expand aliases
- Capital `C` reverse color cycle
- `W` new worktree session (in sidebar, not just grid)
- Settings modal keymap (`esc/s/R/j/k/arrows`)
- Title-edit `esc` cancel
- Orphan picker + recovery picker bindings
- Multi-input `esc` cancel in filter/name/custom command

`README.md` only references headline keys.

## Test surface

`internal/tui/flow_*.go` and `keys_test.go` call `SendKey` / `SendSpecialKey` with `tea.KeyEnter` / `tea.KeyEscape` / literal strings in 70+ places. Any default-key change requires either:
- Updating the test helpers to resolve keys through the configured `KeyMap`, OR
- Accepting that default-value changes require mass test updates

The former is the cleaner path for the long term.

## User constraints (clarified 2026-04-14)

- **Confirm / Cancel / "don't show again" in dialogs are NOT configurable.** `y`, `enter`, `esc`, `n`, `d` stay hard-coded literals inside dialog/overlay handlers. Rationale: muscle memory and accessibility for confirm/cancel must be universal; making them rebindable invites footguns.
- **Cursor navigation is one consistent set across sidebar AND grid.** A single `CursorUp/Down/Left/Right` (configurable) is used in both contexts. Defaults: arrow keys (likely with `h/j/k/l` aliases — TBD).
- **Reorder is one consistent set across sidebar AND grid.** A single `MoveUp/Down/Left/Right` (configurable) is used in both contexts. Defaults: `shift+arrow`.
- Implication: GridKeyMap's hard-coded NavUp/Down/Left/Right (`keys.go:158-161`) and the existing sidebar `MoveUp/Down/Left/Right` config fields collapse into the unified cursor + move actions. The grid's separate "input mode" cursor handling and the sidebar's project-jump don't change scope here — they're separate actions.
- **Exit-grid-input is bound to the same key as Detach.** Single configurable "leave-current-context" binding. The current `ctrl+q` literal at `gridview.go:227` and any hard-coded grid-exit go away in favor of `kb.Detach`.

## Proposed scope phases

Given the breadth, consider staging:

**Phase 1 — Centralize + fix the user-visible issues**
- Remove literal `"enter"` from Attach; add it as a configurable alt-binding.
- Expose InputMode (`i`), SessionColorNext/Prev (`v`/`V`), ToggleAll (`G`) in `KeybindingsConfig`.
- Move ToggleCollapse, FocusToggle, Confirm, Cancel, InputMode into KeybindingsConfig with sensible defaults.
- Add migration in `internal/config/migrate.go`.

**Phase 2 — Component keymaps**
- Pass KeyMap into settings, orphanpicker, recoverypicker, help, gridview components.
- Replace `msg.String()` literal switches with `key.Matches(msg, km.X)` calls.

**Phase 3 — Settings UI editor**
- Build a keybinding editor tab in the settings modal so users can rebind in-app.

**Phase 4 — Docs + policy**
- Regenerate `docs/keybindings.md` from the KeyMap (or at least audit against it).
- Add an AC policy (to `AGENTS.md`) requiring new keys to update config + docs.

## Open questions

- Should we introduce a `[]string` per action (multiple keys) in config, or keep the current `string` convention? Multi-key would handle cases like `Attach = enter/a` cleanly but changes the config schema.
- Where should the AC policy live: `AGENTS.md`, a new `docs/keybindings-policy.md`, or the feature template?
- For Phase 3 (in-app rebinding UI), is a conflict-detection story in-scope?
