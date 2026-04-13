# Feature: Fix arrow key navigation in sidepanel view

- **GitHub Issue:** #100
- **Stage:** DONE
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

Arrow keys no longer navigate the sidepanel view, while vim-style keys (h, j, k, l) still work but are undocumented. This suggests sloppy or inconsistent key handling in the sidepanel component. Arrow keys should work alongside vim-style keys, and all navigation keys should be documented in the help system.

## Research

Root cause identified: commit `72db4d2` changed default `NavUp`/`NavDown` bindings from vim-style "k"/"j" to "up"/"down", but no config migration was added. Users with a saved config retain their old "k"/"j" values, so arrow keys aren't in any binding and silently do nothing. Vim keys still work because they're in the persisted config.

Additionally, `CollapseItem` and `ExpandItem` are hardcoded to `"left"` and `"right"` in `keys.go` with no vim aliases (h/l), creating an inconsistency.

### Relevant Code
- `internal/tui/keys.go:56-57` — `CollapseItem`/`ExpandItem` hardcoded to "left"/"right", no h/l alias
- `internal/tui/keys.go:59-60` — `NavUp`/`NavDown` built from config; if config has old "k"/"j", arrow keys aren't bound at all
- `internal/config/defaults.go:75-76` — new defaults say "up"/"down" for NavUp/NavDown (changed in #79)
- `internal/config/migrate.go` — has migrations for other fields but missing one for NavUp/NavDown when old vim values are present
- `internal/tui/handle_keys.go:480-484` — sidebar navigation dispatches via `key.Matches` which only fires if the key is in the binding

### Constraints / Dependencies
- The keybindings config is user-persisted; simply changing defaults.go doesn't fix existing installs
- `CollapseItem`/`ExpandItem` are not in `KeybindingsConfig` (hardcoded only), so they can't be in config migration — must be fixed in `keys.go`
- Fix must not break users who intentionally set custom nav keys

## Plan

Fix arrow key navigation in two parts: (1) make canonical arrow keys permanent aliases in the key binding layer so they always work regardless of user config, and (2) add vim-style h/l aliases for collapse/expand to complete the vim navigation set. Add a Settings reset-to-defaults shortcut so users can recover clean keybindings.

### Files to Change
1. `internal/tui/keys.go` — Add `uniqueKeys(keys ...string) []string` helper to deduplicate. Change `NavUp`/`NavDown` to always include "up"/"down" as permanent aliases alongside the configured key (`uniqueKeys(kb.NavUp, "up")`). Add "h"/"l" as vim aliases to `CollapseItem`/`ExpandItem` hardcoded bindings. Update help text for CollapseItem/ExpandItem to show `←/h` and `→/l`.

2. `internal/tui/components/settings.go` — In `Update()`, add `case "R":` that resets `sv.cfg.Keybindings = config.DefaultConfig().Keybindings` and marks dirty. Update the hint bar in `View()` to show `R: reset keys`.

3. `docs/keybindings.md` — Document h/l as collapse/expand aliases; note that j/k work when configured via NavUp/NavDown; document the Settings R=reset shortcut.

4. `CHANGELOG.md` — Add `[Unreleased]` Fixed entry for arrow key regression and vim-style aliases.

### Test Strategy
- `internal/tui/flow_navigation_test.go` (new file or extend existing) — `TestNavArrowKeysWork`: use `flowRunner`, call `SendSpecialKey(tea.KeyUp)` and `SendSpecialKey(tea.KeyDown)`, assert `AssertActiveSession` changes — verifies arrow keys always navigate regardless of config.
- Extend same file — `TestCollapseExpandVimKeys`: send "h" with a project selected, assert it collapses; send "l", assert it expands.
- `internal/tui/components/settings_test.go` — `TestSettingsResetKeybindings`: Open settings with modified keybindings, send "R", verify `sv.GetConfig().Keybindings == config.DefaultConfig().Keybindings` and `sv.IsDirty() == true`.

### Risks
- If a user has deliberately bound NavUp to a letter that conflicts with another action (e.g. "n"), the alias adding only appends "up" — no conflict introduced.
- Adding "h" to CollapseItem could shadow any future h-key feature. Document it.
- The "R" key in Settings must not conflict — it's currently unused there (checked: no `case "r"` or `case "R"` in settings Update).

## Implementation Notes

- Added `uniqueKeys` helper in `keys.go` to deduplicate binding slices; used for NavUp/NavDown aliases.
- Arrow keys ("up"/"down") are permanently included in NavUp/NavDown bindings via `uniqueKeys(kb.NavUp, "up")` — no config migration needed since the alias approach covers all saved configs.
- Added "h"/"l" as permanent hardcoded aliases for CollapseItem/ExpandItem in `keys.go`.
- Settings `R` key resets keybindings; also added `j`/`k` as aliases for up/down navigation within settings (`case "down", "j":` / `case "up", "k":`).
- Added `SidebarView: "s"` keybinding (configurable) that focuses sidebar pane in main view and closes grid in grid view.
- Golden file `TestGolden_HelpOverlay.golden` updated to reflect new `←/h`, `→/l` and `s sidebar view` entries.

- **PR:** #102
