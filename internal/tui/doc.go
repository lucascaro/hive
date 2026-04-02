// Package tui implements the Bubble Tea TUI for Hive.
//
// The root [Model] follows the Elm MVU (Model-View-Update) architecture provided
// by Bubble Tea. All application state is held in Model.appState ([state.AppState])
// and is mutated exclusively inside Update() via reducers from internal/state.
//
// # Key files
//
//   - app.go      — [Model] struct, [New], [Init], [Update], [View]
//   - messages.go — all [tea.Msg] types used across the app
//   - keys.go     — [KeyMap] loaded from [config.KeybindingsConfig]
//   - layout.go   — terminal size tracking and pane width calculations
//   - persist.go  — [SaveState] / [LoadState] for state.json
//
// # Sub-packages
//
//   - components/ — individual UI components (Sidebar, Preview, StatusBar, etc.)
//   - styles/     — shared Lip Gloss theme
//
// # Update() routing
//
// All messages flow through Model.Update(). Component updates are dispatched
// by calling their own Update() methods. State reducers (state.*) are the only
// way to mutate Model.appState.
//
// # Startup
//
// cmd/start.go calls [New](cfg, appState) to construct the root model, then
// passes it to tea.NewProgram.
package tui
