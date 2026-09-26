//go:build !windows

package plugin

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ownGroup puts cmd in a new process group, so killTree reaches every
// process it forks, not only the leader.
func ownGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killTree SIGKILLs p's whole process group. p must have been started
// with ownGroup, so its pgid is its pid.
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// termTree asks p's process group to exit, giving a plugin the chance
// to flush before killTree follows.
func termTree(p *os.Process) {
	if p != nil {
		_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	}
}
