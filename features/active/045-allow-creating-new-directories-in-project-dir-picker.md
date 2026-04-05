# Feature: Allow creating new directories in the project dir picker

- **GitHub Issue:** #45
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

When creating a new project, the directory picker only allows selecting existing directories. For brand new projects where the directory doesn't exist yet, users should be able to create a new directory directly from the picker.

**Expected behavior:** The directory picker should offer an option to create a new directory (e.g., typing a path that doesn't exist and confirming creation).

**Current behavior:** Only existing directories can be selected, requiring the user to manually create the directory outside of hive before setting up the project.

## Research

The dirpicker is a self-contained Bubble Tea component that renders a modal overlay for browsing directories. Adding "create new directory" can be done entirely within the component using an inline text input.

### Relevant Code
- `internal/tui/components/dirpicker.go:42-49` — DirPicker struct; needs new fields for create-dir state and textinput
- `internal/tui/components/dirpicker.go:142-175` — Key handling; add new key binding (e.g. `+` or `n`) to enter create mode
- `internal/tui/components/dirpicker.go:116-133` — `loadDir()` creates directory listing; call after creating new dir to refresh
- `internal/tui/components/dirpicker.go:179-209` — `View()` rendering; add inline text input when in create mode
- `internal/tui/components/dirpicker_test.go` — Existing tests for key consumption, navigation, filtering
- `internal/tui/components/titleedit.go` — Existing pattern for inline textinput with Start/Stop lifecycle
- `internal/tui/handle_project.go:25-44` — `handleDirPicked()` already handles non-existent dirs with `os.MkdirAll`
- `internal/tui/views.go:50-62` — Overlay modal rendering pattern with lipgloss styling
- `internal/tui/flow_project_test.go:10-116` — Integration tests for full project creation flow

### Constraints / Dependencies
- The `bubbles/textinput` package is already used elsewhere in the codebase (titleedit, settings, name input)
- Must not conflict with the list's built-in `/` filter mode
- Key binding must not collide with existing list navigation keys (↑/↓/j/k/h/enter/esc/.)

## Plan

Add a create-directory mode directly inside the DirPicker component. Press `n` or `+` to enter an inline text input within the overlay, type the directory name, press `enter` to create it, or `esc` to cancel. After creation, the listing refreshes and auto-navigates into the new directory.

### Files to Change
1. `internal/tui/components/dirpicker.go`
   - Add fields: `creating bool`, `createInput textinput.Model`, `createErr error`
   - Initialize `textinput.Model` in `NewDirPicker()` (CharLimit ~255, Width ~50)
   - In `Update()`: when not filtering and `n` or `+` pressed → set `creating=true`, focus input
   - In `Update()`: when `creating==true`, handle `enter` (create dir via `os.MkdirAll`, reload, navigate into it) and `esc` (cancel create mode)
   - In `View()`: when `creating==true`, replace the list area with a "New directory name:" prompt + text input + error display
   - Update footer hint to include `n/+: new dir`
2. `internal/tui/components/dirpicker_test.go`
   - Test `n` and `+` enter create mode and consume keys
   - Test `esc` in create mode cancels without creating
   - Test `enter` in create mode creates the directory (using `t.TempDir()`)
   - Test creating a dir with an empty name is rejected
   - Test creating a dir that already exists shows an error

### Test Strategy
- Unit tests in `dirpicker_test.go` covering the new key flows (uses `t.TempDir()` for filesystem isolation)
- Manual test: full project creation flow — press `n`, type name, enter, navigate with dir picker, press `n`, type new dir name, enter, verify dir created and navigated into, press `.` to confirm

### Risks
- `os.MkdirAll` permission errors on read-only dirs — handled by showing `createErr` in the view

## Implementation Notes

No deviations from plan. All changes self-contained in `dirpicker.go`:
- Added `creating`, `createInput`, `createErr` fields
- `n`/`+` keys enter create mode (only when not filtering)
- `enter` creates the dir via `os.MkdirAll` and navigates into it; empty names are ignored
- `esc` cancels create mode without closing the picker
- View swaps list for text input prompt when in create mode
- Footer hints updated to show `n/+: new dir`

- **PR:** #47
