// Package mux provides a multiplexer abstraction for managing terminal sessions.
// Call SetBackend once at startup (e.g. in cmd/start.go) before using any other
// function in this package.
package mux

import "fmt"

const shortIDLen = 8

// WindowInfo holds information about a window within a session.
type WindowInfo struct {
	Index  int
	Name   string
	Active bool
}

// Backend is the interface that multiplexer implementations must satisfy.
// Both the tmux-based and native PTY backends implement this interface.
type Backend interface {
	// IsAvailable reports whether the backend is available on this system.
	IsAvailable() bool
	// IsServerRunning reports whether the backend's server/daemon is running.
	IsServerRunning() bool

	// CreateSession creates a new session with a first window running cmd.
	CreateSession(session, windowName, workDir string, cmd []string) error
	// SessionExists reports whether a session with the given name exists.
	SessionExists(session string) bool
	// KillSession destroys a session and all its windows.
	KillSession(session string) error
	// ListSessionNames returns the names of all live sessions.
	ListSessionNames() ([]string, error)

	// CreateWindow adds a new window to an existing session and returns its index.
	CreateWindow(session, windowName, workDir string, cmd []string) (int, error)
	// WindowExists reports whether target ("session:index") exists.
	WindowExists(target string) bool
	// KillWindow removes the window at the given target.
	KillWindow(target string) error
	// RenameWindow changes the name of the window at target.
	RenameWindow(target, newName string) error
	// ListWindows returns all windows in a session.
	ListWindows(session string) ([]WindowInfo, error)

	// CapturePane returns the rendered visible content of a window pane.
	// lines specifies scrollback depth (0 = visible only).
	CapturePane(target string, lines int) (string, error)
	// CapturePaneRaw returns content with all escape sequences preserved
	// (used by the title watcher to detect OSC title sequences).
	CapturePaneRaw(target string, lines int) (string, error)
	// GetCurrentCommand returns the name of the foreground process in the pane.
	GetCurrentCommand(target string) (string, error)

	// Attach takes over the current terminal and connects it to the window
	// at target, allowing the user to interact with the running process.
	// Returns when the user detaches (backend-specific detach key) or the
	// process exits.
	Attach(target string) error

	// SupportsPopup reports whether the backend can open an in-UI floating
	// popup overlay for a session (requires tmux ≥ 3.2 and running inside tmux).
	// If false, callers must fall back to a full-screen Attach.
	SupportsPopup() bool

	// PopupAttach opens a floating popup overlay connected to the window at
	// target. Only valid to call when SupportsPopup() returns true.
	// Returns when the popup closes (user detaches or process exits).
	PopupAttach(target string) error

	// UseExecAttach reports whether the TUI should use tea.ExecProcess to run
	// an external attach command rather than quitting and restarting.
	// True for the tmux backend (exec "tmux …"). False for the native backend
	// (which performs attach via Go I/O and requires the quit+restart loop).
	UseExecAttach() bool

	// DetachKey returns a human-readable description of the key sequence used
	// to return to hive from an attached session (e.g. "Ctrl+Q" or "Ctrl+B D").
	DetachKey() string
}

// active is the single backend instance used by all package-level functions.
// Must be set via SetBackend before any other functions are called.
var active Backend

// SetBackend sets the active multiplexer backend. Must be called once at
// startup before any other mux functions. Subsequent calls replace the backend.
func SetBackend(b Backend) {
	active = b
}

// ---- Utility functions (backend-independent) --------------------------------

// SessionName returns the session name for a project.
func SessionName(projectID string) string {
	short := projectID
	if len(short) > shortIDLen {
		short = short[:shortIDLen]
	}
	return fmt.Sprintf("hive-%s", short)
}

// WindowName returns the window name for a session.
func WindowName(sessionID string) string {
	if len(sessionID) > shortIDLen {
		return sessionID[:shortIDLen]
	}
	return sessionID
}

// Target returns the target string "session:window" used to address a pane.
func Target(session string, windowIdx int) string {
	return fmt.Sprintf("%s:%d", session, windowIdx)
}

// ---- Package-level forwarding functions ------------------------------------

// IsAvailable reports whether the active backend is available.
func IsAvailable() bool { return active.IsAvailable() }

// IsServerRunning reports whether the active backend's server is running.
func IsServerRunning() bool { return active.IsServerRunning() }

// CreateSession creates a new session with a first window running cmd.
func CreateSession(session, windowName, workDir string, cmd []string) error {
	return active.CreateSession(session, windowName, workDir, cmd)
}

// SessionExists reports whether a session with the given name exists.
func SessionExists(session string) bool { return active.SessionExists(session) }

// KillSession destroys a session and all its windows.
func KillSession(session string) error { return active.KillSession(session) }

// ListSessionNames returns the names of all live sessions.
func ListSessionNames() ([]string, error) { return active.ListSessionNames() }

// CreateWindow adds a window to an existing session and returns its index.
func CreateWindow(session, windowName, workDir string, cmd []string) (int, error) {
	return active.CreateWindow(session, windowName, workDir, cmd)
}

// WindowExists reports whether target ("session:index") exists.
func WindowExists(target string) bool { return active.WindowExists(target) }

// KillWindow removes the window at the given target.
func KillWindow(target string) error { return active.KillWindow(target) }

// RenameWindow changes the name of the window at target.
func RenameWindow(target, newName string) error { return active.RenameWindow(target, newName) }

// ListWindows returns all windows in a session.
func ListWindows(session string) ([]WindowInfo, error) { return active.ListWindows(session) }

// CapturePane returns the rendered visible content of a pane.
func CapturePane(target string, lines int) (string, error) {
	return active.CapturePane(target, lines)
}

// CapturePaneRaw returns pane content with all escape sequences preserved.
func CapturePaneRaw(target string, lines int) (string, error) {
	return active.CapturePaneRaw(target, lines)
}

// GetCurrentCommand returns the name of the foreground process in the pane.
func GetCurrentCommand(target string) (string, error) {
	return active.GetCurrentCommand(target)
}

// Attach connects the current terminal to the window at target.
func Attach(target string) error { return active.Attach(target) }

// SupportsPopup reports whether the active backend can show an in-UI popup overlay.
// Returns false if no backend has been set (e.g. in tests).
func SupportsPopup() bool {
	if active == nil {
		return false
	}
	return active.SupportsPopup()
}

// PopupAttach opens a floating popup overlay for the window at target.
// Only call when SupportsPopup() returns true.
func PopupAttach(target string) error { return active.PopupAttach(target) }

// UseExecAttach reports whether the TUI should use tea.ExecProcess for attach.
// Returns false if no backend has been set (e.g. in tests), causing the TUI to
// fall back to the quit+restart path.
func UseExecAttach() bool {
	if active == nil {
		return false
	}
	return active.UseExecAttach()
}

// DetachKey returns a description of the key sequence used to return to hive.
func DetachKey() string { return active.DetachKey() }
