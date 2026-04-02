// Package escape parses OSC 2 terminal title sequences and the Hive-specific
// title marker from PTY output, and provides a background watcher that detects
// title changes in active sessions.
//
// # Title formats
//
// Two formats are supported:
//
//   - OSC 2 (standard xterm):  \033]2;title\007  or  \033]2;title\033\\
//   - Hive marker:             \x00HIVE_TITLE:title\x00
//
// # Usage
//
// [ParseTitle] extracts the title string from raw PTY output:
//
//	title, ok := escape.ParseTitle(rawOutput)
//
// [Watcher] polls [mux.CapturePaneRaw] on a ticker and dispatches
// [tui.SessionTitleChangedMsg] when a title sequence is detected. It is
// started during TUI initialization in tui/app.go.
package escape
