# Feature: Consolidate hotkey definitions and display between sidebar and grid modes

- **GitHub Issue:** #79
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Branch:** —

## Description

Hotkey handling and display is currently duplicated and inconsistent between sidebar mode and grid mode — the same action may use different keys, appear in one help view but not the other, or be formatted differently. This leads to drift as new keys are added.

Consolidate hotkey definitions into a single source of truth using the `bubbles/key` package (`key.Binding` with built-in Help metadata), so each action is defined once with its keys + description. Both the sidebar and grid views should render their help/hotkey hints from the same bindings, automatically staying in sync. Mode-specific bindings (e.g. grid-only reorder keys) should still be expressible, but shared bindings should not be redefined.

## Research

### Summary
Three separate locations hardcode key strings independently of `KeyMap`, causing drift. The fix is to route all hint rendering through `m.keys` (the `KeyMap`), which already has proper `key.Binding` entries with `.Help()` metadata.

### Relevant Code

**Single source of truth (already exists):**
- `internal/tui/keys.go:9-42` — `KeyMap` struct with all `key.Binding` entries. Every action has `.Help().Key` and `.Help().Desc` available. Lives in `tui` package.

**Three locations with hardcoded key strings:**

1. `internal/tui/views.go:82-128` — `helpView()` has a local `[]binding{{key, desc}}` slice with hardcoded strings like `"j/k ↑↓"`, `"enter/a"`, etc. The `Model` has `m.keys` available here — straightforward fix.

2. `internal/tui/components/statusbar.go:124-179` — `buildHints()` builds a local `[]hint` slice with hardcoded strings (`"a/↵"`, `"g/G"`, `"c/C"`, etc.). **Package boundary problem**: `statusbar.go` is in `components/`; `KeyMap` is in `tui/`. Direct reference would create an import cycle.

3. `internal/tui/components/gridview.go:274` — A single hardcoded string: `"←→↑↓/hjkl: navigate   S-←/→: reorder   enter/a: attach   x: kill   r: rename   c/C: color   v/V: session color   G: all   esc/g/q: exit"`. Same package-boundary issue.

**Key handler for reference:**
- `internal/tui/handle_keys.go:76-200` — `handleGridKey()` shows which keys are grid-only vs shared. Grid-only: reorder (MoveLeft/MoveRight), `g`/`G` toggle, `x` kill, `r` rename, `c`/`C`/`v`/`V` color, `W` worktree, `t` new session.

### Preferred Approach: Use `bubbles/help` Component

`github.com/charmbracelet/bubbles/help` provides exactly this abstraction:
- `help.KeyMap` interface: implement `ShortHelp() []key.Binding` and `FullHelp() [][]key.Binding` on `KeyMap`
- `help.Model.View(keyMap)` renders one-line short help or multi-column full help
- `ShowAll bool` toggles between modes
- Automatically skips disabled bindings, handles truncation with `…` when `Width` is set
- Built-in adaptive styling (light/dark terminal)

**Package boundary solution (resolved):**
The `help.Model` lives on `tui.Model` (not in components). Components receive pre-computed strings:
- `StatusBar.View()` accepts a pre-computed hint string from `m.help.View(m.keys)`
- Grid view hint line accepts a pre-computed string from a grid-scoped help model
- `helpView()` in `views.go` uses `m.help.View(m.keys)` with `ShowAll = true`

**Separate KeyMaps needed:**
- `KeyMap` in `keys.go` → add `ShortHelp()` / `FullHelp()` methods → drives sidebar statusbar + full help overlay
- `GridKeyMap` (new, small subset) → `ShortHelp()` returns grid-only bindings → drives grid hint line

### Constraints / Dependencies
- Must update these golden files (intentional, run with `-update`):
  - `testdata/TestGolden_HelpOverlay.golden` — now uses bubbles/help multi-column layout
  - `testdata/TestGolden_DefaultView*.golden` — statusbar hint line format changes
  - `testdata/TestGolden_GridView_*.golden` — grid hint line derives from `GridKeyMap`
- Grid-only bindings (reorder, v/V session color, W worktree) must be in `GridKeyMap`, not `KeyMap.ShortHelp()`
- Config-remapped keys will automatically appear in all hint locations after this change
- `bubbles` is already a direct dependency (used for `key.Binding`) — no new dependency needed

## Plan

Two goals rolled together: (1) use `bubbles/help` as the single rendering engine for key hints so the source of truth is `KeyMap`; (2) remove vim-style (`j/k/h/l`) and WASD (`w/a/s/d`) navigation aliases and make most actions consistent across sidebar and grid.

### Consolidated Key Binding Table (after this change)

| Key | Sidebar | Grid | Notes |
|-----|---------|------|-------|
| `↑` / `↓` | navigate sessions | navigate cells | only arrow keys; `k`/`j` removed |
| `←` / `→` | collapse / expand | navigate cells | `h`/`l` removed |
| `shift+↑/↓` | reorder item | reorder session | same binding works both views |
| `shift+←/→` | reorder item | reorder session | same binding works both views |
| `enter` / `a` | attach | attach | shared |
| `t` | new session | new session | shared |
| `x` | kill | kill | shared; `d` alias removed |
| `r` | rename | rename | shared |
| `c` / `C` | project color | project color | shared |
| `v` / `V` | session color | session color | shared |
| `W` | new worktree | new worktree | shared |
| `n` | new project | — | sidebar only |
| `T` | new team | — | sidebar only |
| `g` | open project grid | toggle → project grid / exit | shared intent |
| `G` | open all-grid | toggle → all-grid / exit | shared intent |
| `?` | help | help | **newly consistent** |
| `S` | settings | settings | **newly consistent** |
| `H` | tmux help | tmux help | **newly consistent** |
| `q` | quit | quit | **changed**: was exit-grid-only |
| `esc` | — | exit grid | grid-only |
| `space` | toggle collapse | — | sidebar only |
| `K` / `J` | jump project | — | sidebar only |
| `1`–`9` | jump project | — | sidebar only |
| `/` | filter | — | sidebar only |
| `ctrl+p` | palette | — | sidebar only |

### Files to Change

1. **`internal/config/defaults.go`** — Change `NavUp: "k"` → `NavUp: "up"`, `NavDown: "j"` → `NavDown: "down"`. Vim keys removed from defaults; users who want them can set via config.

2. **`internal/tui/keys.go`** — Four changes:
   - Remove hardcoded `"up"` alias from `NavUp` binding and `"down"` from `NavDown` (config default now provides them; no duplicate needed)
   - Remove hardcoded `"d"` alias from `KillSession`
   - Add `ShortHelp() []key.Binding` method to `KeyMap` — returns sidebar hint bindings in priority order
   - Add `FullHelp() [][]key.Binding` method to `KeyMap` — returns grouped columns for the full help overlay (all bindings)
   - Add `GridKeyMap` struct (or type alias) with its own `ShortHelp()` returning the grid-relevant subset

3. **`internal/tui/app.go`** — Add `helpModel help.Model` and `gridHelpModel help.Model` fields. Initialize in `New()`. In `mainView()`, compute hints string via `m.helpModel.View(m.keys)` and pass to `m.statusBar.View(...)`. In `View()` for grid, compute grid hints and pass to `m.gridView.View(...)`.

4. **`internal/tui/components/statusbar.go`** — Change `StatusBar.View()` signature to accept `hints string` as the last parameter. Remove `buildHints()` function and the `hint` type entirely. Filter/confirm state hint lines stay as special cases in `app.go` before calling `statusBar.View()`.

5. **`internal/tui/components/gridview.go`** — Three changes:
   - Remove `"h"`, `"w"`, `"k"` aliases from up/left/right navigation cases; remove `"l"`, `"d"`, `"s"` aliases
   - Change `View()` to accept `hints string` parameter; replace hardcoded `hintLine2` string
   - Remove `"q"` from `gv.Hide()` case (leave only `"esc"`; `q` will be handled as quit in `handleGridKey`)

6. **`internal/tui/handle_keys.go`** — In `handleGridKey()`, before delegating to `gv.Update()`, add explicit handling for:
   - `?` → `m.PushView(ViewHelp)` (help overlay)
   - `S` → open settings
   - `H` → `m.PushView(ViewTmuxHelp)`
   - `q` → `tea.Quit` (quit app, not just exit grid)
   (`esc` / `g` / `G` continue to exit grid as before)

7. **`internal/tui/views.go`** — Replace `helpView()`'s hardcoded `[]binding` slice with `m.helpModel` rendered at `ShowAll = true`. Wrap the result in the same `lipgloss.Place` + border. Set `m.helpModel.Width` before calling.

8. **`internal/tui/components/settings.go`** — Remove `"j"`, `"k"`, `"h"`, `"l"`, `"s"` navigation aliases from switch cases; arrow keys only.

9. **`internal/tui/components/orphanpicker.go`** — Remove `"k"`, `"j"` aliases; arrow keys only.

10. **`internal/tui/components/recoverypicker.go`** — Remove `"k"`, `"j"`, `"h"`, `"l"` aliases; arrow keys only.

11. **`internal/tui/components/dirpicker.go`** — Remove `"h"` alias from the `left`/backspace case; arrow key only.

12. **`internal/tui/flow_*.go` test files** — Update all `SendKey("j")`, `SendKey("k")`, `SendKey("l")`, `SendKey("s")` calls to use arrow key equivalents (`"down"`, `"up"`, `"right"`, `"down"`). Affected: `flow_bell_test.go`, `flow_grid_test.go`, `flow_reorder_test.go`, `flow_session_test.go`, `flow_settings_test.go` (~44 occurrences).

13. **`internal/tui/testdata/*.golden`** — Regenerate: `go test ./internal/tui/ -run TestGolden -update`

14. **`docs/keybindings.md`** — Update to reflect: arrow-only navigation, removed `d` kill alias, grid now supports `?`/`S`/`H`/`q`, removed vim/WASD references in both sidebar and grid sections.

### Test Strategy

- **`internal/tui/keys_test.go`** — `TestKeyMap_ShortHelp`: asserts non-empty, all bindings have non-empty Key+Desc. `TestKeyMap_FullHelp`: asserts ≥2 columns, all `KeyMap` fields covered. `TestGridKeyMap_ShortHelp`: asserts grid-specific bindings (reorder, v/V) appear; nav bindings present.

- **`internal/tui/components/statusbar_test.go`** — Update existing tests to pass a `hints` string argument. Add `TestStatusBar_PassedHints`: verify the passed string appears verbatim in `line2`.

- **`internal/tui/flow_grid_test.go`** — Add `TestFlow_GridQuitQuitsApp`: open grid, press `q`, assert program returned `tea.Quit`. Add `TestFlow_GridHelpOpen`: open grid, press `?`, assert `ViewHelp` is top view.

- **Golden tests** — All `TestGolden_*` must pass after running `-update`. No separate test needed.

### Risks

- **Config migration**: Existing users who rely on `k`/`j` defaults will lose them after upgrade. Mitigate: note the change prominently in `CHANGELOG.md`.
- **`helpView()` layout change**: `bubbles/help` columnar layout differs from current hand-rolled list. Set `m.helpModel.Width` and `ShowAll = true`; golden test will validate visual result.
- **Settings component vim keys**: Settings has its own vim-key navigation for tabs/rows. These are internal to `settings.go` and not driven by `KeyMap`; removing them is a straight deletion.
- **`d` alias removal**: Some existing users may use `d` to kill sessions. The removal is breaking; document in `CHANGELOG.md`.

## Implementation Notes

- Used `bubbles/help` component as the rendering engine for all hint locations; `KeyMap` implements `help.KeyMap` via `ShortHelp()`/`FullHelp()` methods.
- `GridKeyMap` struct provides grid-specific hint subset with consolidated navigation labels (e.g., `↑↓←→` collapsed to one entry).
- Package boundary resolved by having `app.go` compute hints via `helpModel.View(m.keys)` and pass pre-computed strings to `StatusBar.View()` and `GridView.View()`.
- `buildHints()` and `hint` type removed entirely from `statusbar.go` — no longer needed.
- Status legend still appended to sidebar hints in `buildStatusHints()` in `app.go`.
- Filter-active and preview-pane states handled as special-case strings in `buildStatusHints()` (not routed through `help.Model`).
- All 5 test files with vim/WASD key usage updated (~44 SendKey call sites).
- `TestDirPicker_HKeyGoesUp` removed (arrow key equivalent already tested in `TestDirPicker_LeftArrowGoesUp`).
- `TestSettingsView_SwitchTab_HLMatchesArrows` test now only tests arrows (name is now misleading but behavior is correct).
- Golden test files regenerated with `-update`.
- All 277 tests pass.

- **PR:** —
