//go:build !windows

package muxnative

import (
	"os/exec"
	"syscall"
)

// daemonCmd builds an exec.Cmd that will run detached from the current terminal
// session (Setsid=true). stdin/stdout/stderr are nil so the daemon does not
// inherit the controlling terminal.
func daemonCmd(exe string, args ...string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd
}
