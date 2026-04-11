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
type Backend struct {
	spec mux.DetachKeySpec
}

// NewBackend returns a new tmux-based Backend that installs spec as the
// in-attach detach binding via `bind-key -n` (set up by AttachScript).
func NewBackend(spec mux.DetachKeySpec) *Backend { return &Backend{spec: spec} }

func (b *Backend) IsAvailable() bool    { return tmux.IsAvailable() }
func (b *Backend) IsServerRunning() bool { return tmux.IsServerRunning() }

// CreateSession creates a tmux session. The agent command is wrapped in a shell
// so that "tmux detach-client" runs when the process exits, returning the user
// to hive automatically.
func (b *Backend) CreateSession(session, windowName, workDir string, cmd []string) error {
	if err := tmux.CreateSession(session, windowName, workDir, b.wrapCmd(cmd)); err != nil {
		return err
	}
	// Enable mouse support so users can scroll through output.
	// Non-fatal: don't fail session creation over a cosmetic option.
	_ = tmux.SetOption(session, "mouse", "on")
	// Ensure bell monitoring is on so #{window_bell_flag} tracks bells.
	_ = tmux.SetOption(session, "monitor-bell", "on")
	return nil
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

func (b *Backend) GetPaneTitles(session string) (map[string]string, map[string]bool, error) {
	return tmux.GetPaneTitles(session)
}

func (b *Backend) CapturePane(target string, lines int) (string, error) {
	return tmux.CapturePane(target, lines)
}

func (b *Backend) CapturePaneRaw(target string, lines int) (string, error) {
	return tmux.CapturePaneRaw(target, lines)
}

func (b *Backend) GetCurrentCommand(target string) (string, error) {
	return tmux.GetCurrentCommand(target)
}

func (b *Backend) IsPaneDead(target string) bool { return tmux.IsPaneDead(target) }

// DetachKey returns the configured detach key in human-readable form
// (e.g. "Ctrl+Q"). The actual binding is installed by AttachScript.
func (b *Backend) DetachKey() string { return b.spec.Display }

// UseExecAttach returns true: the tmux backend attaches by running an external
// tmux command, which is suitable for tea.ExecProcess.
func (b *Backend) UseExecAttach() bool { return true }
// Requires tmux ≥ 3.2 AND that hive is currently running inside a tmux session
// (i.e. the TMUX environment variable is set).
func (b *Backend) SupportsPopup() bool {
	return os.Getenv("TMUX") != "" && tmux.SupportsDisplayPopup()
}

// PopupAttach opens a floating tmux popup overlay connected to the window at
// target. The popup fills 95 % of the terminal width and 90 % of its height.
// If title is non-empty it is displayed in the popup border via -T.
// It closes automatically when the attached session detaches or the process exits.
//
// The popup runs the shared attach script (via `sh -c`) so it picks up the
// configured single-key detach binding and the custom Hive status bar.
func (b *Backend) PopupAttach(target, title string) error {
	script := b.AttachScript(target, title)

	args := []string{"display-popup",
		"-E", // close popup when command exits
		"-w", "95%",
		"-h", "90%",
	}
	if title != "" {
		args = append(args, "-T", " "+title+" ")
	}
	args = append(args, "--", "sh", "-c", script)
	cmd := exec.Command("tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Attach runs the shared attach script so the headless `hive attach` CLI
// honors the same single-key detach binding as the TUI's attach path.
// The user detaches with the configured key (default Ctrl+Q); tmux's own
// `Ctrl+B D` continues to work as a fallback because we add a binding
// rather than replacing the prefix.
func (b *Backend) Attach(target string) error {
	script := b.AttachScript(target, "")
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sessionFromTarget extracts the session name from a tmux target string of
// the form "session:window". When the target has no colon (already a bare
// session name) it is returned unchanged.
func sessionFromTarget(target string) string {
	if i := strings.IndexByte(target, ':'); i >= 0 {
		return target[:i]
	}
	return target
}

// wrapCmd wraps an agent command in a shell so "tmux detach-client" runs when
// the agent exits, returning the user to hive.
// Each argument is single-quote-escaped to prevent shell injection.
func (b *Backend) wrapCmd(cmd []string) []string {
	quoted := make([]string, len(cmd))
	for i, arg := range cmd {
		quoted[i] = shellQuote(arg)
	}
	return []string{"sh", "-c", strings.Join(quoted, " ") + "; tmux detach-client"}
}

// shellQuote wraps s in POSIX single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
