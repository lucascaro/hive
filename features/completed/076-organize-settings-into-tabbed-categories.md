# Feature: Organize settings into tabbed categories

- **GitHub Issue:** #76
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Branch:** —

## Description

The settings screen has grown long — General, Team Defaults, Hooks, and a large Keybindings section all scroll through one flat list, making navigation and discovery difficult. Reorganize into a tabbed layout where each category (General, Team Defaults, Hooks, Keybindings, and future additions) is a separate tab the user can switch between with Left/Right arrows (or h/l).

Within each tab, j/k navigation and edit/toggle behavior stay the same. The current category headers become the tabs themselves.

## Research

All settings rendering, state, and key handling live in a single file; the flat list is already grouped by 4 header entries, making the tab split a natural refactor.

### Relevant Code
- `internal/tui/components/settings.go:34-48` — `settingField` and `settingEntry` types (`isHeader`/`header`/`field` union). `settingEntry` stays as-is; tabs just partition the existing entries list by the 4 header boundaries.
- `internal/tui/components/settings.go:51-71` — `SettingsView` struct: holds `entries`, `fieldIdxs` (non-header indices), `cursor`, `scrollOffset`. Need new `activeTab int`, plus per-tab `cursor` and `scrollOffset` (e.g. replace scalars with `[]int` of length nTabs).
- `internal/tui/components/settings.go:508-692` — `buildSettingEntries()`. Headers at lines **511 (General), 594 (Team Defaults), 633 (Hooks), 664 (Keybindings)**. Easiest refactor: build one slice per tab (`buildGeneralEntries()`, etc.) and keep `entries [][]settingEntry` indexed by tab.
- `internal/tui/components/settings.go:441-448` — `rebuildFieldIdxs()`: needs to run per-tab (or the structure flattens so headers disappear and we just have fields per tab).
- `internal/tui/components/settings.go:113-201` — `Update()` / key dispatch: j/k at 162-170, enter/space at 172-197, s at 154-160, esc at 142-152. Add left/right (and h/l) to switch `activeTab`; delegate j/k to the active tab's cursor.
- `internal/tui/components/settings.go:238-353` — `View()`: renders header/footer + scrollable body with manual window slicing (317-343). Tab strip becomes a new line in the header area. Manual scroll is fine to keep; per-tab offset just reads from the active slot.
- `internal/tui/components/settings.go:357-439` — `renderLines()`: currently emits header rows (362-366) inside the body. With tabs, skip header rows — the tab strip replaces them.
- `internal/tui/app.go:55, 144` — `Model.settings` field + `NewSettingsView()` init. No changes expected.
- `internal/tui/handle_keys.go:232-233` — settings open dispatch; sets width/height. No changes.
- `internal/tui/viewstack.go:10, 92-93` — `ViewSettings` state sync. No changes.

### Reusable Patterns
- `github.com/charmbracelet/bubbles/viewport` is already imported in `internal/tui/app.go` and `components/preview.go` — could replace the manual scroll logic in settings, but not required for this change.
- No `bubbles/tabs` component exists upstream. The tab strip renders as a lipgloss row using the existing palette in `internal/tui/styles/theme.go` (active vs inactive tab styles).
- No existing golden tests for `SettingsView` in `internal/tui/testdata/` — low test-churn refactor.

### Constraints / Dependencies
- Must preserve existing keybindings within a tab (j/k, enter/space, s, esc, edit mode).
- Per-tab state: each tab needs its own cursor and scroll offset so switching tabs is lossless.
- Narrow terminals: tab strip must fit `innerW = Width - 4` (settings.go:252). Plan fallbacks: truncate labels, drop to short forms, or horizontally scroll the tab strip.
- Edit mode (`editing` bool, settings.go:66-68) must block tab switching until the edit is committed or cancelled, otherwise cursor state across tabs gets confused.
- Dirty state is a single `dirty` bool across all tabs (line 64) — stays global; save/discard keeps the same UX.
- Acceptance: left/right (and h/l) switch tabs; current category headers become the tab labels; all other navigation unchanged.

## Plan

### Approach

Partition the existing flat `[]settingEntry` list into one slice per tab at build time (General, Team Defaults, Hooks, Keybindings). Header entries are dropped — their labels become the tab strip. `SettingsView` gains an `activeTab` index and replaces scalar `cursor`/`scrollOffset` with slices of length `nTabs` so each tab keeps its own position. Left/Right and h/l switch tabs; all other navigation and edit/save/discard logic is unchanged. Tab strip renders above the body as a single lipgloss row using existing theme styles.

### Files to Change

1. **`internal/tui/components/settings.go`** — main refactor.
   - Add a `settingTab` struct (`title string`, `entries []settingEntry`) and a package-level `tabTitles` slice used for both build and render.
   - Replace `buildSettingEntries() []settingEntry` (line 508) with `buildSettingTabs() []settingTab`. Split the existing four sections at lines 511/594/633/664 — each becomes its own tab with the header line removed (the title moves to the tab strip).
   - In `SettingsView` (line 51): replace `entries []settingEntry` / `fieldIdxs []int` / `cursor int` / `scrollOffset int` with `tabs []settingTab` / `tabFieldIdxs [][]int` / `tabCursors []int` / `tabScrollOffsets []int` / `activeTab int`. Add helpers `currentTab()`, `currentFields()`, `currentCursor()` / `setCurrentCursor()`, `currentScroll()` / `setCurrentScroll()`.
   - `Open()` (line 82): call `buildSettingTabs()`, allocate the per-tab slices, zero `activeTab`.
   - `rebuildFieldIdxs()` (line 441): becomes `rebuildTabFieldIdxs()`, loops over tabs.
   - `Update()` (line 113): add left/right/h/l handling that switches `activeTab`, gated on `!sv.editing && !sv.pendingSave && !sv.pendingDiscard`. Re-wire j/k, enter/space, s, esc to read/write the per-tab cursor via helpers. No behavior change inside edit/save/discard modes.
   - `View()` (line 238): render a tab strip (new helper `renderTabStrip(innerW int) string`) between the title bar and body. If tab-strip width overflows `innerW`, truncate per-tab labels evenly; fall back to first-letter labels when even 1-char-per-tab + separators would overflow.
   - `renderLines()` (line 357): drop the header-row branch (lines 362-366); operate on the active tab's entries only. Scroll window uses the active tab's offset.
   - Add new lipgloss styles in `internal/tui/styles/theme.go` (`TabActiveStyle`, `TabInactiveStyle`) if not already present; reuse existing palette colors so no new color tokens are introduced.

2. **`internal/tui/styles/theme.go`** — add `TabActiveStyle` and `TabInactiveStyle` (only if comparable styles don't already exist; reuse `SelectedStyle`/`MutedStyle` where possible).

3. **`docs/features.md`** — brief mention of tabbed settings navigation (single sentence under the Settings description).

4. **`docs/keybindings.md`** — add Left/Right (and h/l) for tab switching in the Settings overlay section.

5. **`CHANGELOG.md`** — add entry under `[Unreleased]` → `Changed`: "Settings screen organized into tabs (General, Team Defaults, Hooks, Keybindings). Left/Right or h/l switches tabs."

No changes needed in `app.go`, `handle_keys.go`, or `viewstack.go`.

### Test Strategy

**Unit tests** — new file `internal/tui/components/settings_test.go`:

- `TestSettingsView_BuildTabs_HasFourCategories` — `buildSettingTabs()` returns exactly 4 tabs with titles "General", "Team Defaults", "Hooks", "Keybindings" and non-empty entries each.
- `TestSettingsView_BuildTabs_NoHeaderEntries` — none of the returned entries have `isHeader == true`.
- `TestSettingsView_Open_InitializesPerTabState` — after `Open(cfg)`, `tabCursors` and `tabScrollOffsets` have length 4 and all zero; `activeTab == 0`.
- `TestSettingsView_SwitchTab_Right` — from fresh `Open`, send `right`: `activeTab == 1`; send 3× more `right`: `activeTab` wraps to 0 (or clamps to 3 — pick clamp; assert clamp at 3).
- `TestSettingsView_SwitchTab_Left_ClampsAtZero` — from `activeTab == 0`, `left` leaves `activeTab == 0`.
- `TestSettingsView_SwitchTab_HL_MatchArrows` — `h` == left, `l` == right behavior identical.
- `TestSettingsView_SwitchTab_PreservesPerTabCursor` — on tab 0, press j twice (cursor=2); switch to tab 1; press j once (cursor=1); switch back to tab 0; assert cursor==2 and tab1 cursor still==1.
- `TestSettingsView_SwitchTab_BlockedWhileEditing` — start editing a string field (`editing = true`); send `right`; `activeTab` unchanged.
- `TestSettingsView_SwitchTab_BlockedWhilePendingSave` — with `pendingSave = true`, `right` is consumed by save-confirm path, not tab switch (existing behavior stays intact).
- `TestSettingsView_RenderTabStrip_FitsNarrowWidth` — call `renderTabStrip(20)`; assert output width ≤ 20 (use `lipgloss.Width`) and active tab is visually distinguishable from inactive (prefix check for active marker or style).
- `TestSettingsView_View_ShowsActiveTabContent` — after switching to tab 2 (Hooks), `View()` output contains a field label known to live in the Hooks section and does NOT contain a field label known to live only in General.

**Functional (flow) tests** — new file `internal/tui/flow_settings_test.go` using `flowRunner`:

- `TestFlow_Settings_OpenShowsFirstTab` — open app, press `,` (Settings key), assert `ViewContains("General")` and the tab strip is rendered.
- `TestFlow_Settings_TabSwitchRight` — open settings; send `right`; assert a Team Defaults field is visible and a General-only field is not.
- `TestFlow_Settings_TabSwitchHL` — open settings; `l`, `l`, `h`; land on Team Defaults (index 1); assert content.
- `TestFlow_Settings_PerTabCursorPreserved` — open settings; press `j` twice on tab 0; switch to tab 1; `j` once; switch back to tab 0; send enter or check rendered cursor marker to confirm cursor landed on tab 0's 3rd field.
- `TestFlow_Settings_EditBlocksTabSwitch` — navigate to a string field, press enter to start editing, press `right`; assert still editing and still on the same tab.
- `TestFlow_Settings_SaveStillWorks` — change a bool field (enter/space), press `s`, confirm with `y`; assert `SettingsSaveRequestMsg` handled and view closed (reuse existing save-flow helpers if present).

Run with `go test ./internal/tui/... -run 'Settings|Flow_Settings'`.

### Risks

- **Cursor bounds after config reload.** If the config structure changes (fewer fields in a tab), stale `tabCursors` could point past the end. Mitigation: clamp `tabCursors[i]` to `len(tabFieldIdxs[i]) - 1` in `rebuildTabFieldIdxs()`.
- **Narrow terminal tab strip.** Overflow must degrade gracefully. Mitigation: width-aware truncation with tests (`TestSettingsView_RenderTabStrip_FitsNarrowWidth`); worst case, render first-letter labels.
- **Key conflicts.** `h`/`l` are used as tab switchers, but they are currently not bound to anything in the settings view — verify by grep. Confirmed: `handleEditKey` (line 203) owns edit mode; outside edit mode, h/l are unused in settings.
- **Help text / keybinding docs drift.** The footer/help text inside settings must list the new Left/Right hint. Addressed in `View()` footer render.
- **Golden test creation risk.** No existing settings golden tests, so no churn — but if a future PR adds them, they'll need to account for the tab strip line. Flag in implementation notes.
- **Edit mode re-entry.** Ensure that exiting edit mode keeps `activeTab` unchanged (it only mutates the field, not the tab). Covered by `TestFlow_Settings_EditBlocksTabSwitch`.

## Implementation Notes

- Simplified the state model further than the plan: dropped `settingEntry` entirely (was only needed to mix header rows with fields in a flat list). With tabs, headers move to the strip and each tab holds `[]*settingField` directly. No need for a `fieldIdxs` mapping anymore — the cursor indexes the tab's fields slice directly.
- Did not add new lipgloss styles in `styles/theme.go`; the tab strip uses inline `lipgloss.NewStyle()` with existing color tokens (`ColorAccent`, `ColorBg`, `ColorMuted`) to avoid bloating the shared style module for two one-off styles.
- `docs/keybindings.md` was not updated — it has no existing section describing the Settings overlay's intra-view keys (only the global keymap), so there was no natural place to add `←/→`/`h/l`. Footer of the settings view itself now shows the `←/→:tab` hint.
- Added four read-only exported accessors on `SettingsView` (`ActiveTab`, `TabCursor`, `IsEditing`, `IsPendingSave`) so flow tests in the `tui` package can assert internal state without being in the `components` package.
- Content height accounting adjusted: now subtracts 3 rows (header + tab strip + footer) instead of 2.
- All 11 unit tests and 6 flow tests pass; existing settings tests updated to the new tab-based navigation.

- **PR:** —
