//go:build !windows

package session

import (
	"github.com/aymanbagabas/go-pty"
)

// newWindowsCmd is a non-Windows stub. It exists so the call site in
// session.go can compile cross-platform; on non-Windows builds the
// Windows branch is gated behind `runtime.GOOS == "windows"` and this
// function is never invoked.
func newWindowsCmd(ptmx pty.Pty, wrapper, line string) *pty.Cmd {
	_ = line
	return ptmx.Command(wrapper)
}
