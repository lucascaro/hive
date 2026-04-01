// Package muxtmux implements the mux.Backend interface using an external tmux binary.
package muxtmux

import (
	"os"
	"os/exec"
	"strings"

	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/tmux"
)

// Backend wraps the tmux CLI to implement mux.Backend.
type Backend struct{}

// NewBackend returns a new tmux-based Backend.
func NewBackend() *Backend { return &Backend{} }

func (b *Backend) IsAvailable() bool    { return tmux.IsAvailable() }
func (b *Backend) IsServerRunning() bool { return tmux.IsServerRunning() }

// CreateSession creates a tmux session. The agent command is wrapped in a shell
// so that "tmux detach-client" runs when the process exits, returning the user
// to hive automatically.
func (b *Backend) CreateSession(session, windowName, workDir string, cmd []string) error {
	return tmux.CreateSession(session, windowName, workDir, b.wrapCmd(cmd))
}

func (b *Backend) SessionExists(session string) bool { return tmux.SessionExists(session) }
func (b *Backend) KillSession(session string) error  { return tmux.KillSession(session) }
func (b *Backend) ListSessionNames() ([]string, error) { return tmux.ListSessionNames() }

// CreateWindow adds a window to an existing tmux session. The agent command is
// wrapped with "tmux detach-client" so the user returns to hive when done.
func (b *Backend) CreateWindow(session, windowName, workDir string, cmd []string) (int, error) {
	return tmux.CreateWindow(session, windowName, workDir, b.wrapCmd(cmd))
}

func (b *Backend) WindowExists(target string) bool { return tmux.WindowExists(target) }
func (b *Backend) KillWindow(target string) error  { return tmux.KillWindow(target) }
func (b *Backend) RenameWindow(target, newName string) error {
	return tmux.RenameWindow(target, newName)
}

func (b *Backend) ListWindows(session string) ([]mux.WindowInfo, error) {
	ws, err := tmux.ListWindows(session)
	if err != nil {
		return nil, err
	}
	out := make([]mux.WindowInfo, len(ws))
	for i, w := range ws {
		out[i] = mux.WindowInfo{Index: w.Index, Name: w.Name, Active: w.Active}
	}
	return out, nil
}

func (b *Backend) CapturePane(target string, lines int) (string, error) {
	return tmux.CapturePane(target, lines)
}

func (b *Backend) CapturePaneRaw(target string, lines int) (string, error) {
	return tmux.CapturePaneRaw(target, lines)
}

// DetachKey returns the tmux detach key description.
func (b *Backend) DetachKey() string { return "Ctrl+B D" }

// Attach runs "tmux attach-session -t target", giving the user direct access
// to the tmux window. The user detaches with Ctrl+B D.
func (b *Backend) Attach(target string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// wrapCmd wraps an agent command in a shell so "tmux detach-client" runs when
// the agent exits, returning the user to hive.
func (b *Backend) wrapCmd(cmd []string) []string {
	return []string{"sh", "-c", strings.Join(cmd, " ") + "; tmux detach-client"}
}
