// Package mux provides a multiplexer abstraction for managing terminal sessions.
// Call SetBackend once at startup (e.g. in cmd/start.go) before using any other
// function in this package.
package mux

import "fmt"

const (
	projMaxLen  = 8
	titleMaxLen = 12

	// HiveSession is the single shared tmux session used for all hive windows.
	HiveSession = "hive-sessions"
)

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

	// GetPaneTitles returns pane titles and bell flags for all windows in a session.
	// Returns titles ("session:windowIndex" → pane title) and bells (true when set).
	GetPaneTitles(session string) (map[string]string, map[string]bool, error)


	// CapturePane returns the rendered visible content of a window pane.
	// lines specifies scrollback depth (0 = visible only).
	CapturePane(target string, lines int) (string, error)
	// CapturePaneRaw returns content with all escape sequences preserved
	// (used by the title watcher to detect OSC title sequences).
	CapturePaneRaw(target string, lines int) (string, error)
	// GetCurrentCommand returns the name of the foreground process in the pane.
	GetCurrentCommand(target string) (string, error)
	// IsPaneDead reports whether the pane's process has exited.
	IsPaneDead(target string) bool

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
	// If title is non-empty it is shown in the popup border.
	// Returns when the popup closes (user detaches or process exits).
	PopupAttach(target, title string) error

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

// SessionName returns the shared tmux session name. All hive windows live in
// one session so they are easy to find and recover. The projectID argument is
// ignored; it is kept for call-site compatibility.
func SessionName(_ string) string { return HiveSession }

// WindowName returns a descriptive window name encoding the project, agent
// type, and session title so windows are identifiable in `tmux list-windows`.
// Format: {project[:8]}-{agentType}-{title[:12]}
func WindowName(projectName, agentType, sessionTitle string) string {
	proj := projectName
	if len(proj) > projMaxLen {
		proj = proj[:projMaxLen]
	}
	title := sessionTitle
	if len(title) > titleMaxLen {
		title = title[:titleMaxLen]
	}
	return fmt.Sprintf("%s-%s-%s", proj, agentType, title)
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

// GetPaneTitles returns pane titles and bell flags for all windows in a session.
// Returns nil, nil, nil if no backend has been set (e.g. in tests).
func GetPaneTitles(session string) (map[string]string, map[string]bool, error) {
	if active == nil {
		return nil, nil, nil
	}
	return active.GetPaneTitles(session)
}

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

// IsPaneDead reports whether the pane's process has exited.
func IsPaneDead(target string) bool { return active.IsPaneDead(target) }

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
func PopupAttach(target, title string) error { return active.PopupAttach(target, title) }

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

// AttachScript returns the shell script the active backend uses to attach to
// target with title shown in the status bar. Only the tmux backend produces
// a script (the native backend uses the quit+restart path); for any other
// backend AttachScript returns an empty string. Callers must check
// UseExecAttach() first if they need a non-empty script.
//
// The script is exposed via type-assertion rather than the Backend interface
// to keep the interface free of tmux-specific concerns.
func AttachScript(target, title string) string {
	if as, ok := active.(interface {
		AttachScript(target, title string) string
	}); ok {
		return as.AttachScript(target, title)
	}
	return ""
}
