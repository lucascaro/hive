//go:build !windows

package proc

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// OwnGroup puts cmd in a new process group, so KillTree reaches every
// process it forks, not only the leader.
func OwnGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillTree SIGKILLs p's whole process group. p must have been started
// with OwnGroup, so its pgid is its pid.
func KillTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// TermTree asks p's process group to exit, giving a child the chance
// to flush before KillTree follows.
func TermTree(p *os.Process) {
	if p != nil {
		_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	}
}
