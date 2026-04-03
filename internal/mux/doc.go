// Package mux provides a multiplexer abstraction for managing terminal sessions.
//
// The package exposes a [Backend] interface that both the native PTY backend and
// the tmux backend implement, along with package-level forwarding functions that
// delegate to the active backend. Call [SetBackend] once at startup (cmd/start.go)
// before calling any other function.
//
// # Backend selection
//
// Two implementations exist:
//   - mux/native — built-in PTY daemon (no external dependencies)
//   - mux/tmux   — delegates to the tmux binary
//
// The active backend is chosen in cmd/start.go based on the --native flag or
// config.Multiplexer ("native" | "tmux").
//
// # Addressing
//
// Sessions and windows are addressed by string targets:
//
//	session  = mux.SessionName(projectID)              // always "hive-sessions"
//	window   = mux.WindowName(projName, agent, title)  // "{proj[:8]}-{agent}-{title[:12]}"
//	target   = mux.Target(session, index)              // "hive-sessions:index"
//
// # Thread safety
//
// All package-level functions are safe to call from Bubble Tea's Update() method
// (single-threaded by design). Backend implementations may use internal locking.
package mux
