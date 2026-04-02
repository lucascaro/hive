// Package components provides the Bubble Tea UI components used by the Hive TUI.
//
// Each component follows the standard Bubble Tea pattern:
//   - A struct holding component-local display state
//   - An Update(tea.Msg) method returning (ComponentType, tea.Cmd)
//   - A View() string method for pure rendering with Lip Gloss
//
// Components are wired into the root [tui.Model] in internal/tui/app.go.
// They receive messages via their Update() when the root model's Update() routes
// a message to them.
//
// # Components
//
//   - [Sidebar]      — three-level collapsible project/team/session tree
//   - [Preview]      — live session output with ANSI passthrough (500ms refresh)
//   - [StatusBar]    — breadcrumb path and context-sensitive key hints
//   - [GridView]     — tiled grid overview for a project or all projects
//   - [AgentPicker]  — agent type selection menu (opened by `t` key)
//   - [TeamBuilder]  — multi-step team creation wizard (opened by `T` key)
//   - [TitleEditor]  — inline rename text input (opened by `r` key)
//   - [Confirm]      — yes/no confirmation overlay for destructive actions
//   - [SettingsView] — settings/config overlay
//   - [OrphanPicker] — overlay for cleaning up orphaned tmux sessions
//
// All styles are sourced from internal/tui/styles; components must not
// hardcode ANSI codes or raw colour values.
package components
