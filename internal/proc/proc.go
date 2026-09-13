// Package proc is the single constructor for every non-PTY child
// process Hive spawns.
//
// It exists for one Windows-shaped reason. `hivegui.exe` is a GUI
// binary and `hived.exe` is started detached (see
// cmd/hivegui/window_windows.go), so neither owns a console. When a
// process with no console starts a console-subsystem child — git, gh,
// tasklist, `claude --version` — Windows hands that child a brand new
// console *window*, which flashes on screen and takes focus. Hive
// shells out constantly (worktree inventory alone runs several git
// commands per poll), so on Windows ordinary use produced a stream of
// popups.
//
// Command and CommandContext return the same *exec.Cmd os/exec would,
// marked so Windows creates no console for it. Off Windows there is
// nothing to suppress and they are a pass-through.
//
// PTY-backed children do not belong here: internal/session attaches
// those to a ConPTY, which never has a window of its own.
// TestNoDirectExecOnWindows enforces the rule for everything else.
package proc

import (
	"context"
	"os/exec"
)

// Command is exec.Command with no console window on Windows.
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	HideConsole(cmd)
	return cmd
}

// CommandContext is exec.CommandContext with no console window on
// Windows.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	HideConsole(cmd)
	return cmd
}
