# Feature: Show confirmation dialog when saving settings

- **GitHub Issue:** #93
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

When saving settings in the settings view, the current confirmation feedback is shown in the status bar, which is easy to miss and not prominent enough. Users may not notice whether their settings were saved or not.

The save confirmation should instead appear as a modal dialog, making it immediately clear that the action completed successfully. This improves discoverability and gives the save action the visual weight it deserves as a potentially impactful operation.

## Research

The current save flow lives entirely in the settings component. When the user presses `s` while dirty, `pendingSave = true`; the footer then shows "Save to [path]? y/enter: confirm  any other key: cancel". The next keypress either sends `SettingsSaveRequestMsg` or clears the flag.

The existing `ViewConfirm` / `Confirm` dialog infrastructure is fully built and used for kill-session, kill-project, etc. It works via `ConfirmActionMsg` → push `ViewConfirm` → `ConfirmedMsg{Action}` → `handleConfirmedAction(action)`.

### Relevant Code
- `internal/tui/components/settings.go:69` — `pendingSave bool` field; lines 201-213 handle the inline confirmation; lines 369-381 render it in the footer
- `internal/tui/components/confirm.go` — `Confirm` struct with `Message`, `Action`; renders a rounded-border modal dialog with y/n hints
- `internal/tui/viewstack.go:15` — `ViewConfirm` ID; line 104 syncs `appState.ShowConfirm`
- `internal/tui/handle_system.go:104` — `handleConfirmAction` sets up confirm and pushes `ViewConfirm`; line 113 `handleConfirmed` dispatches to `handleConfirmedAction`
- `internal/tui/operations.go:436` — `handleConfirmedAction(action string)` dispatches on string prefix; add "save-settings" here
- `internal/tui/app.go:41` — `Model` struct; has `confirm components.Confirm` and `pendingWorktree*` pattern to follow for storing config pending save
- `internal/tui/app.go:322` — `Update` switch; add case for `components.SettingsSaveConfirmMsg`
- `internal/tui/messages.go:102` — `ConfirmActionMsg` and `ConfirmedMsg` types

### Constraints / Dependencies
- `components` package must not import `tui` package (circular import); a new message type `SettingsSaveConfirmMsg` defined in `components` will carry the pending config to the app layer
- `handleCancelled` already pops the top view (ViewConfirm) cleanly, leaving ViewSettings intact — no changes needed there

## Plan

Replace the inline `pendingSave` status-bar prompt in `SettingsView` with the existing `ViewConfirm` modal dialog system. Settings emits a new message type on save; the app stores the pending config and pushes `ViewConfirm`; confirm dispatches `SettingsSaveRequestMsg`.

### Files to Change

1. `internal/tui/components/settings.go`
   - Add `SettingsSaveConfirmMsg{Config config.Config}` type (and `var _ tea.Msg` compile-check)
   - Remove `pendingSave bool` from `SettingsView` struct
   - Remove `sv.pendingSave = false` from `Open()` and `Close()`
   - Remove `IsPendingSave() bool` method
   - Remove the `if sv.pendingSave { ... }` block in `Update()` (lines 201–213)
   - Change `case "s":` handler (line 238): instead of `sv.pendingSave = true`, return `func() tea.Msg { return SettingsSaveConfirmMsg{Config: sv.cfg} }`; don't call `sv.Close()` here
   - Remove `pendingSave` branch from `View()` footer (lines 369–374)

2. `internal/tui/app.go`
   - Add `pendingSaveConfig config.Config` field to `Model` struct (next to other `pending*` fields)
   - Add `case components.SettingsSaveConfirmMsg:` in `Update()` switch → `m.handleSettingsSaveConfirm(msg)`

3. `internal/tui/handle_system.go`
   - Add `handleSettingsSaveConfirm(msg components.SettingsSaveConfirmMsg)`:
     - Store `m.pendingSaveConfig = msg.Config`
     - Set `m.appState.ConfirmMsg` and `m.appState.ConfirmAction = "save-settings"`
     - Set `m.confirm.Message = fmt.Sprintf("Save settings to %s?", config.ConfigPath())` and `m.confirm.Action = "save-settings"`
     - Call `m.PushView(ViewConfirm)`

4. `internal/tui/operations.go` — `handleConfirmedAction`
   - Add at the top: `if action == "save-settings" { cfg := m.pendingSaveConfig; return func() tea.Msg { return components.SettingsSaveRequestMsg{Config: cfg} } }`

5. `internal/tui/components/settings_test.go`
   - Update `TestSettingsView_PendingSaveConfirm`: pressing 's' while dirty should emit `SettingsSaveConfirmMsg`, not `SettingsSaveRequestMsg`
   - Update/remove `TestSettingsView_SaveCancelledByOtherKey`: cancel is now handled by ViewConfirm, not settings
   - Update/remove `TestSettingsView_SwitchTab_BlockedWhilePendingSave`: behavior moved to ViewConfirm layer

6. `internal/tui/flow_settings_test.go`
   - `TestFlow_Settings_SaveStillWorks`: replace `IsPendingSave()` check with `TopView() == ViewConfirm` check; add `ExecCmdChain` after 's' to push dialog; press 'y', then `ExecCmdChain` chain
   - `TestFlow_Settings_SaveError_PopsViewAndShowsError`: same pattern

7. `internal/tui/flow_bell_test.go`
   - `TestFlow_BellSoundInSettings`: adjust save-flow assertion to match new two-step dispatch

8. `CHANGELOG.md` — add `[Unreleased]` entry under `Changed`

### Test Strategy

- `TestSettingsView_SaveEmitsConfirmMsg` (new/renamed in `settings_test.go`) — pressing 's' while dirty returns `SettingsSaveConfirmMsg` with correct config
- `TestFlow_Settings_SaveStillWorks` (updated) — full flow: 's' → ExecCmd → ViewConfirm on stack; 'y' → ExecCmdChain → settings closed, ViewMain
- `TestFlow_Settings_SaveCancelStillWorks` (new in `flow_settings_test.go`) — 's' → ExecCmd → ViewConfirm; 'n' → ViewSettings still on top
- `TestFlow_Settings_SaveError_PopsViewAndShowsError` (updated) — same setup, save fails → ViewMain, LastError set
- `TestFlow_BellSoundInSettings` (updated) — verify new two-step dispatch carries config through

### Risks

- Existing tests deeply tied to `pendingSave` state — must update carefully
- `handleCancelled` resets unrelated fields (`pendingWorktree`, etc.) — no issue, those are orthogonal
- If user presses 's' again while confirm is already shown — ViewConfirm is already on stack; settings key won't be reached (keys route to top view only)

## Implementation Notes

- Removed `pendingSave bool` from `SettingsView` entirely; no `IsPendingSave()` method remains
- `SettingsSaveConfirmMsg{Config}` defined in `components` to avoid circular import with `tui`
- `pendingSaveConfig config.Config` added to `Model` to hold staged config while dialog is open
- `handleConfirmedAction("save-settings")` retrieves config from `m.pendingSaveConfig` — no encoding in action string needed
- Cancelling the dialog (n/esc) pops `ViewConfirm` via `handleConfirm`, leaving `ViewSettings` intact — no special handling needed
- 7 test functions updated across 3 test files; 2 new tests added (`TestFlow_Settings_SaveCancel`, `TestSettingsView_SStillActiveAfterSPress`)

- **PR:** —
