//go:build windows

package proc

import (
	"os"
	"os/exec"
	"strconv"
)

// OwnGroup is a no-op on Windows: taskkill /T walks the process tree
// by parent pid instead of a group. (HideConsole already set this
// cmd's SysProcAttr, which a Setpgid-style overwrite would clobber.)
func OwnGroup(*exec.Cmd) {}

// KillTree force-kills p and every process it started. A child that
// re-parented itself away (a daemonised grandchild) escapes; plugins and ACP
// adapters are asked not to do that.
func KillTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
	if err != nil {
		// taskkill fails when the process is already gone; make sure
		// the leader at least is dead either way.
		_ = p.Kill()
	}
	return nil
}

// TermTree has no graceful equivalent for a windowless process tree on
// Windows, so the kill that follows it is the only signal.
func TermTree(*os.Process) {}
