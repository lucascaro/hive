//go:build !windows

package muxtmux

import (
	"errors"
	"syscall"
)

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// ESRCH = no such process → dead. EPERM = process exists but not ours
	// (treat as alive; safer than killing a tmux session we don't own).
	return !errors.Is(err, syscall.ESRCH)
}
