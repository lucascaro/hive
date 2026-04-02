// Package state provides the pure data model and reducer functions for Hive.
//
// This package holds the single source of truth for the application state ([AppState])
// and a set of pure, side-effect-free reducer functions in store.go. All state
// mutations must go through these reducers; no package should mutate AppState fields
// directly.
//
// # Data hierarchy
//
//	AppState
//	  └── []*Project
//	        ├── []*Team        (orchestrator + worker sessions)
//	        │     └── []*Session
//	        └── []*Session     (standalone)
//
// # Key types
//
//   - [AppState] — root state held by the Bubble Tea model; never shared across goroutines
//   - [Project]  — named group of sessions and teams
//   - [Team]     — coordinated agent group with one orchestrator and N workers
//   - [Session]  — maps 1:1 to a multiplexer window (tmux window or native PTY pane)
//
// # Usage pattern
//
// Callers (only tui/app.go) call reducers like this:
//
//	newState, sess := state.CreateSession(&appState, projectID, title, ...)
//	appState = *newState
//
// This package has no external dependencies beyond the standard library.
package state
