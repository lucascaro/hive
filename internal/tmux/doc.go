// Package tmux provides low-level wrappers around the tmux CLI binary.
//
// These helpers are used exclusively by the mux/tmux backend. They shell out
// to the tmux binary and parse its output. If you are looking for the multiplexer
// abstraction, see internal/mux.
//
// # Files
//
//   - session.go  — CreateSession, KillSession, ListSessionNames
//   - window.go   — CreateWindow, KillWindow, RenameWindow, ListWindows
//   - capture.go  — CapturePane (rendered output), CapturePaneRaw (with escape sequences)
//   - client.go   — Attach (connect current terminal to a pane)
//   - names.go    — Naming and target formatting helpers
package tmux
