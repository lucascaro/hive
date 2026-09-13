//go:build windows

package proc_test

import (
	"context"
	"os/exec"
	"syscall"
	"testing"

	"github.com/lucascaro/hive/internal/proc"
)

const createNoWindow = 0x08000000

// The whole point of the package: a child spawned from hivegui or
// hived — neither of which owns a console — must not be given a
// console window.
func TestCommandSuppressesTheConsoleWindow(t *testing.T) {
	for name, cmd := range map[string]*exec.Cmd{
		"Command":        proc.Command("git", "status"),
		"CommandContext": proc.CommandContext(context.Background(), "git", "status"),
	} {
		if cmd.SysProcAttr == nil {
			t.Errorf("%s: SysProcAttr is nil; the child would get a console window", name)
			continue
		}
		if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
			t.Errorf("%s: CreationFlags = %#x, want CREATE_NO_WINDOW set", name, cmd.SysProcAttr.CreationFlags)
		}
		if !cmd.SysProcAttr.HideWindow {
			t.Errorf("%s: HideWindow = false, want true", name)
		}
	}
}

// HideConsole adds to whatever the caller already set rather than
// replacing it — clobbering a caller's flags is how a detached spawn
// would quietly lose DETACHED_PROCESS.
func TestHideConsolePreservesExistingFlags(t *testing.T) {
	const createNewProcessGroup = 0x00000200
	cmd := exec.Command("git", "status")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	proc.HideConsole(cmd)
	if cmd.SysProcAttr.CreationFlags&createNewProcessGroup == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP preserved", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NO_WINDOW added", cmd.SysProcAttr.CreationFlags)
	}
}
