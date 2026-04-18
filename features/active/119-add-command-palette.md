# Feature: Add command palette

- **GitHub Issue:** #119
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Branch:** —

## Description

Hive has a `ctrl+p` keybinding configured for a "command palette" action, but no palette feature exists yet — pressing the key does nothing. A command palette would let users quickly search and invoke any action (attach, kill, rename, new session, etc.) by name, without memorizing keybindings. This is especially valuable as the number of configurable actions grows.

## Research

### Existing Infrastructure

**Palette keybinding already wired:**
- `internal/tui/keys.go:30` — `Palette key.Binding` in KeyMap struct
- `internal/config/config.go:199` — `Palette KeyBinding` config field, default `["ctrl+p"]`
- `internal/tui/keys.go:169` — appears in FullHelp
- **No handler exists** — pressing ctrl+p does nothing

**Reusable picker pattern (AgentPicker as template):**
- `internal/tui/components/agentpicker.go` — wraps `charmbracelet/bubbles/list` with manual filtering, `enter` → emit `AgentPickedMsg`, `esc` → cancel. All pickers (AgentPicker, DirPicker, OrphanPicker, RecoveryPicker) follow this exact pattern.
- View stack integration via `PushView`/`PopView` + `syncLegacyFlags` in `viewstack.go`
- `list.New()` with filtering disabled, manual `applyFilter()` on character input

**Available actions to expose (~25):**
- Navigation: Quit, QuitKill, Help, TmuxHelp, Settings, Filter, GridOverview, ToggleAll, SidebarView
- Create: NewProject, NewSession, NewWorktreeSession, NewTeam
- Edit: Attach, Rename, ColorNext/Prev, SessionColorNext/Prev, KillSession, KillTeam
- Layout: ToggleCollapse, CollapseItem, ExpandItem

**Key handler routing:**
- `handle_keys.go:280-551` — `handleGlobalKey()` has the full action switch
- Palette handler goes after Filter (line 313), before SidebarView (line 315)
- Pattern: `case key.Matches(msg, m.keys.Palette): m.PushView(ViewPalette); return m, nil`

### Relevant Code
- `internal/tui/components/agentpicker.go` — template picker pattern to clone
- `internal/tui/keys.go:30,112` — Palette binding already defined
- `internal/tui/handle_keys.go:280-551` — action dispatch to wire palette results into
- `internal/tui/viewstack.go:8-28` — ViewID constants, PushView/PopView, syncLegacyFlags
- `internal/tui/app.go:182-206` — Model struct, picker field initialization

**Shortcut display for learning:**
- Each `key.Binding` exposes `.Help().Key` (e.g. "enter") and `.Help().Desc` (e.g. "attach") — `keys.go:88`
- Palette items should show the shortcut key on the right side of each row (e.g. `Attach session          enter`) so users learn bindings
- The `list.DefaultDelegate` supports `Title` + `Description` fields — shortcut can go in `Description`

### Constraints / Dependencies
- All actions need human-readable labels and optional descriptions for the list
- Some actions are context-dependent (e.g. KillSession needs a selected session, NewSession needs a project) — palette must handle precondition failures gracefully
- The `charmbracelet/bubbles/list` dependency is already in go.mod — no new deps needed

## Plan

Clone the AgentPicker pattern to create a `CommandPalette` component. Each palette item represents an action from the KeyMap, showing the action name and its current shortcut. Selecting an item dispatches the corresponding action as a `tea.Msg`.

### Design Decisions

- **Item model:** Each palette entry has a `Title` (action name, e.g. "Attach session"), `Description` (shortcut key, e.g. "enter"), and an `Action` string that maps to the KeyMap field name.
- **Shortcut display:** Each row shows the bound key(s) on the right so users learn bindings organically.
- **Action dispatch:** Palette emits `CommandPalettePickedMsg{Action string}`. The handler in `app.go` maps the action string to the same code path as the direct keybinding (e.g. "attach" → same logic as `key.Matches(msg, m.keys.Attach)`).
- **Context-aware items:** Some actions only make sense in certain contexts (e.g. KillSession needs a selected session). The palette shows all actions but gracefully no-ops if preconditions aren't met (same behavior as pressing the key directly).
- **Filtering:** Manual character-by-character filtering (same as AgentPicker), matching on Title.

### Files to Change

1. `internal/tui/components/commandpalette.go` (NEW) — `CommandPalette` struct wrapping `list.Model`. Methods: `Show(items)`, `Hide()`, `Update(msg)`, `View()`. Emits `CommandPalettePickedMsg{Action}` on enter, `CancelledMsg{}` on esc. Items built from KeyMap bindings with action name + shortcut display.
2. `internal/tui/viewstack.go` — Add `ViewPalette` constant. Add `syncLegacyFlags` case.
3. `internal/tui/app.go` — Add `palette components.CommandPalette` field to Model. Initialize in `New()`.
4. `internal/tui/handle_keys.go` — Add `case key.Matches(msg, m.keys.Palette):` in `handleGlobalKey()` to open the palette. Add `case ViewPalette:` in `handleKey()` to route keys to the palette.
5. `internal/tui/app.go` (Update) — Add `case components.CommandPalettePickedMsg:` handler that maps action string to the corresponding function call (reuse existing handler code).
6. `internal/tui/messages.go` — Define `CommandPalettePickedMsg` with compile-time check.
7. `docs/keybindings.md` — Document `ctrl+p` opens command palette.
8. `CHANGELOG.md` — Add entry under `[Unreleased]`.

### Test Strategy

- `internal/tui/flow_palette_test.go`:
  - `TestPalette_OpenAndClose` — press ctrl+p, verify ViewPalette pushed; press esc, verify popped.
  - `TestPalette_SelectAction` — open palette, press enter on "Attach", verify `CommandPalettePickedMsg` dispatched and attach flow triggered.
  - `TestPalette_FilterNarrowsItems` — open palette, type "new", verify only NewProject/NewSession/NewTeam items visible.
  - `TestPalette_ShortcutsShown` — open palette, verify View() output contains the shortcut keys for displayed items.
  - `TestPalette_EscClearsFilterFirst` — type a filter, press esc, verify filter cleared but palette stays open; press esc again, verify palette closed.

### Risks

- **Action mapping maintenance:** Adding a new keybinding requires also adding a palette entry. Mitigated by the AGENTS.md keybindings policy which already requires updating multiple surfaces.
- **Large action list:** ~25 actions may need grouping or smart ordering (most-used first). Start with alphabetical; can add usage-based sorting later.
- **Context sensitivity:** Some actions silently no-op without a selected session. The palette doesn't filter these out — same UX as pressing the key directly.

## Implementation Notes

- Cloned AgentPicker pattern exactly as planned.
- ~25 actions exposed with shortcut labels from KeyMap.Help().Key.
- Action dispatch via string-based switch in handle_palette.go — maps each action to the same code path as its direct keybinding.
- No new dependencies — reuses existing charmbracelet/bubbles/list.

- **PR:** —
