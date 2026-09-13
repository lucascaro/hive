//go:build !windows

package proc

import "os/exec"

// HideConsole is a no-op off Windows: no other platform invents a
// window for a child process.
func HideConsole(*exec.Cmd) {}
