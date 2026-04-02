//go:build windows

package muxnative

import "os/exec"

// daemonCmd builds a detached exec.Cmd on Windows.
// On Windows we use CREATE_NEW_PROCESS_GROUP so the daemon is not killed when
// the parent console closes. The native backend is not functional on Windows,
// but this stub allows the package to compile.
func daemonCmd(exe string, args ...string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd
}
