//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the child runs as a console
// application with no console window. Windows ignores it when it is
// combined with DETACHED_PROCESS or CREATE_NEW_CONSOLE (it is not
// rejected, simply dropped), and DETACHED_PROCESS already leaves the
// child with no console at all — so the detached spawns in
// cmd/hivegui are exempt from the rule rather than routed through here,
// because routing them would buy nothing.
const createNoWindow = 0x08000000

// HideConsole marks cmd so Windows gives it no console window. Callers
// that build their own *exec.Cmd use this; everything else gets it from
// Command / CommandContext.
func HideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	// Belt and braces: CREATE_NO_WINDOW stops Windows allocating a
	// console, but a child that calls AllocConsole itself gets one
	// anyway, shown according to the startup info. HideWindow sets
	// STARTF_USESHOWWINDOW | SW_HIDE, so that console stays hidden too.
	cmd.SysProcAttr.HideWindow = true
}
