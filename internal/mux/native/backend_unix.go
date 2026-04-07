//go:build !windows

// Package muxnative implements the mux.Backend interface using native Go PTY
// management via a persistent background daemon. No external tmux binary is required.
//
// Architecture:
//   - A daemon process (hive mux-daemon) owns all PTY master fds and persists
//     across TUI restarts.
//   - The Backend is a thin client that speaks to the daemon over a Unix socket.
//   - Sessions survive hive exits as long as the daemon is running.
package muxnative

import (
	"errors"

	"github.com/lucascaro/hive/internal/mux"
)

// Backend implements mux.Backend by delegating all operations to the mux daemon.
type Backend struct {
	client *daemonClient
}

// NewBackend returns a new native PTY Backend connected to the daemon at sockPath.
// Call EnsureRunning first to guarantee the daemon is up.
func NewBackend(sockPath string) *Backend {
	return &Backend{client: newDaemonClient(sockPath)}
}

// IsAvailable always returns true — no external binary needed.
func (b *Backend) IsAvailable() bool { return true }

// IsServerRunning reports whether the daemon is reachable.
func (b *Backend) IsServerRunning() bool { return Ping(b.client.sockPath) == nil }

func (b *Backend) CreateSession(session, windowName, workDir string, cmd []string) error {
	_, err := b.client.do(Request{
		Op:         "create_session",
		Session:    session,
		WindowName: windowName,
		WorkDir:    workDir,
		Cmd:        cmd,
	})
	return err
}

func (b *Backend) SessionExists(session string) bool {
	resp, err := b.client.do(Request{Op: "session_exists", Session: session})
	return err == nil && resp.Bool
}

func (b *Backend) KillSession(session string) error {
	_, err := b.client.do(Request{Op: "kill_session", Session: session})
	return err
}

func (b *Backend) ListSessionNames() ([]string, error) {
	resp, err := b.client.do(Request{Op: "list_session_names"})
	if err != nil {
		return nil, err
	}
	return resp.Strings, nil
}

func (b *Backend) CreateWindow(session, windowName, workDir string, cmd []string) (int, error) {
	resp, err := b.client.do(Request{
		Op:         "create_window",
		Session:    session,
		WindowName: windowName,
		WorkDir:    workDir,
		Cmd:        cmd,
	})
	if err != nil {
		return 0, err
	}
	return resp.Int, nil
}

func (b *Backend) WindowExists(target string) bool {
	resp, err := b.client.do(Request{Op: "window_exists", Target: target})
	return err == nil && resp.Bool
}

func (b *Backend) KillWindow(target string) error {
	_, err := b.client.do(Request{Op: "kill_window", Target: target})
	return err
}

func (b *Backend) RenameWindow(target, newName string) error {
	_, err := b.client.do(Request{Op: "rename_window", Target: target, NewName: newName})
	return err
}

func (b *Backend) ListWindows(session string) ([]mux.WindowInfo, error) {
	resp, err := b.client.do(Request{Op: "list_windows", Session: session})
	if err != nil {
		return nil, err
	}
	out := make([]mux.WindowInfo, len(resp.Windows))
	for i, w := range resp.Windows {
		out[i] = mux.WindowInfo{Index: w.Index, Name: w.Name, Active: w.Active}
	}
	return out, nil
}

func (b *Backend) CapturePane(target string, lines int) (string, error) {
	resp, err := b.client.do(Request{Op: "capture_pane", Target: target, Lines: lines})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (b *Backend) CapturePaneRaw(target string, lines int) (string, error) {
	resp, err := b.client.do(Request{Op: "capture_pane_raw", Target: target, Lines: lines})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (b *Backend) GetPaneTitles(_ string) (map[string]string, error) {
	return nil, nil
}

func (b *Backend) GetCurrentCommand(_ string) (string, error) {
	// Native backend does not expose pane process names.
	return "", nil
}

func (b *Backend) IsPaneDead(_ string) bool { return false }

func (b *Backend) Attach(target string) error {
	return clientAttach(b.client, target)
}

// DetachKey returns the native backend detach key description.
func (b *Backend) DetachKey() string { return "Ctrl+Q" }

// UseExecAttach returns false: the native backend attaches via Go-level socket
// I/O, not an external command, so tea.ExecProcess is not applicable.
func (b *Backend) UseExecAttach() bool { return false }

// SupportsPopup always returns false for the native backend.
// The native backend uses its own PTY daemon; tmux display-popup is not applicable.
func (b *Backend) SupportsPopup() bool { return false }

// PopupAttach is not implemented for the native backend.
// Always returns an error; callers must check SupportsPopup first.
func (b *Backend) PopupAttach(_, _ string) error {
	return errors.New("popup attach is not supported by the native PTY backend")
}
