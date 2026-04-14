# Feature: Add tabs to help panel with comprehensive usage and feature reference

- **GitHub Issue:** #105
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

The current help panel only displays a key bindings list, leaving users without a comprehensive reference for Hive's features. Add a tabbed interface to the help panel where "Keys" is the default tab (preserving current behavior), alongside new tabs covering a feature overview and a full usage guide documenting all Hive functionality — views, sessions, projects, grid mode, settings, and agent workflows.

## Research

### Relevant Code
- `internal/tui/views.go:82–98` — `helpView()`: current overlay using Bubble Tea `help.Model`; renders `m.helpModel.View(m.keys)` inside a centered rounded-border box
- `internal/tui/views.go:100–139` — `tmuxHelpView()`: separate hardcoded tmux shortcut overlay using same centered border pattern
- `internal/tui/app.go:117–120` — `helpModel` and `gridHelpModel` fields (Bubble Tea `help.Model`); initialized via `newStyledHelp()`
- `internal/tui/app.go:413–416` — `View()` dispatch: `case ViewHelp` → `m.helpView()`, `case ViewTmuxHelp` → `m.tmuxHelpView()`
- `internal/tui/handle_keys.go:30–34` — ViewHelp/ViewTmuxHelp key dispatch: only handles `esc` or `m.keys.Help` to close; no tab nav
- `internal/tui/components/settings.go:55–80` — reference tab implementation: `settingTab` struct, `activeTab int`, `tabCursors/tabScrollOffsets []int`
- `internal/tui/components/settings.go:232–240` — tab navigation in settings: `left` decrements, `right` increments `activeTab`
- `internal/tui/components/settings.go:483–631` — `renderTabStrip(w)`: raised-capsule 3-row design; graceful degradation on narrow terminals
- `internal/tui/viewstack.go:12–13` — `ViewHelp` and `ViewTmuxHelp` constants; `syncLegacyFlags` sets `m.appState.ShowHelp`
- `internal/tui/keys.go:30–32` — `Help` and `TmuxHelp` key bindings; part of `ShortHelp`/`FullHelp` KeyMap
- `internal/tui/styles/theme.go` — `ColorAccent`, `ColorBg`, `ColorBorder`, `ColorSubtext`, `HelpKeyStyle`, `HelpDescStyle`, `MutedStyle`, `TitleStyle`
- `internal/tui/golden_test.go:102–109` — `TestGolden_HelpOverlay`: pushes ViewHelp, snapshots full view
- `internal/tui/testdata/TestGolden_HelpOverlay.golden` — snapshot to regenerate after changes

### Constraints / Dependencies
- `help.Model` (Bubble Tea bubbles) is purpose-built for KeyMap rendering; custom tab content (Usage, Features) must be rendered manually, not via `help.Model`
- ViewTmuxHelp is currently a separate view — unify it into the tabbed help panel as a "tmux Keys" sub-tab or separate tab, rather than keeping two separate ViewIDs
- `m.appState.ShowHelp` legacy flag must be kept in sync via `viewstack.go:syncLegacyFlags`
- Golden test snapshot will need regeneration after any visual change
- ANSI width handling via `charmbracelet/x/ansi` is required for tab strip to handle escape sequences correctly on narrow terminals

## Plan

Unify the current `helpView()` and `tmuxHelpView()` into a single tabbed `HelpPanel` component with four tabs: Keys (default), tmux, Usage, and Features. Add tab navigation (←/→) and vertical scroll (j/k) inside the panel. The `H` key now opens the help panel at the tmux tab; `?` opens it at the Keys tab.

### Files to Change

1. `internal/tui/components/help.go` (**NEW**) — `HelpPanel` struct with:
   - Fields: `ActiveTab int`, `ScrollOffset int`, `Width int`, `Height int`, internal `help.Model` for Keys tab
   - `NewHelpPanel(h help.Model) HelpPanel` — constructor
   - `Open(tab int)` — set initial tab and reset scroll
   - `Update(msg tea.KeyMsg) bool` — handles left/right (tab nav), j/k/up/down (scroll); returns true if consumed
   - `View(km KeyMap) string` — renders the full tabbed panel centered overlay
   - Private helpers: `renderTabStrip(w int)`, `renderKeysTab(km KeyMap)`, `renderTmuxTab()`, `renderUsageTab()`, `renderFeaturesTab()`
   - Tab content is static text (no config needed); `renderKeysTab` uses the embedded `help.Model`

2. `internal/tui/app.go` — add `helpPanel components.HelpPanel` to `Model` struct; initialize via `components.NewHelpPanel(newStyledHelp())` in `New()`; keep `helpModel` and `gridHelpModel` unchanged (still used for statusbar hint line)

3. `internal/tui/views.go` — replace `helpView()` body: set `m.helpPanel.Width`/`Height`, return `m.helpPanel.View(m.keys)`; delete `tmuxHelpView()` entirely (content moves to `renderTmuxTab`)

4. `internal/tui/app.go` (View switch) — update `case ViewHelp: return m.helpView()` and remove `case ViewTmuxHelp: return m.tmuxHelpView()` (or alias it to `m.helpView()`)

5. `internal/tui/handle_keys.go` — two changes:
   - `case ViewHelp, ViewTmuxHelp:` handler: delegate left/right/j/k/up/down to `m.helpPanel.Update(msg)` before checking esc/close
   - `H` key press: call `m.helpPanel.Open(1)` then `m.PushView(ViewHelp)` (instead of `PushView(ViewTmuxHelp)`)
   - `?` key press: call `m.helpPanel.Open(0)` then `m.PushView(ViewHelp)` (make the tab explicit)

6. `internal/tui/golden_test.go` — rename `TestGolden_HelpOverlay` → `TestGolden_HelpOverlay_Keys`; add `TestGolden_HelpOverlay_Tmux`, `TestGolden_HelpOverlay_Usage`, `TestGolden_HelpOverlay_Features` using `m.helpPanel.Open(n)` before `m.PushView(ViewHelp)`

### Test Strategy

- `TestGolden_HelpOverlay_Keys` (renames existing `TestGolden_HelpOverlay`) — `m.helpPanel.Open(0)`, push ViewHelp, snapshot: verifies Keys tab renders correctly with key bindings table
- `TestGolden_HelpOverlay_Tmux` — `m.helpPanel.Open(1)`, push ViewHelp, snapshot: verifies tmux tab renders the bindings table
- `TestGolden_HelpOverlay_Usage` — `m.helpPanel.Open(2)`, push ViewHelp, snapshot: verifies Usage tab renders static content
- `TestGolden_HelpOverlay_Features` — `m.helpPanel.Open(3)`, push ViewHelp, snapshot: verifies Features tab renders static content
- Regenerate snapshots: `go test ./internal/tui/ -run TestGolden -update`
- Verify `H` key → ViewHelp at tmux tab in `flow_test.go` or a new `TestFlow_TmuxHelpOpensAtTmuxTab`

### Risks

- `help.Model` owned by HelpPanel needs same styles as the one in `app.go`; pass a pre-styled `help.Model` to the constructor to avoid duplication
- Tab strip on narrow terminals: use `ansi.StringWidth`/`ansi.Truncate` like settings does (or simpler truncation since tab count is small)
- Golden snapshot for old `TestGolden_HelpOverlay` must be renamed/deleted; CI will fail until regenerated
- `ViewTmuxHelp` is still pushed nowhere after the change — keep the constant and `syncLegacyFlags` case to avoid compile errors, but it's a dead code path (acceptable or clean up in follow-up)

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** #107 (https://github.com/lucascaro/hive/pull/107)
