//go:build windows

package plugin

import (
	"os"
	"os/exec"
	"strconv"

	"github.com/lucascaro/hive/internal/proc"
)

// ownGroup is a no-op on Windows: taskkill /T walks the process tree
// by parent pid instead of a group. (proc.HideConsole already set this
// cmd's SysProcAttr, which a Setpgid-style overwrite would clobber.)
func ownGroup(*exec.Cmd) {}

// killTree force-kills p and every process it started. A child that
// re-parented itself away (a daemonised grandchild) escapes; plugins
// run with full trust, and docs/plugins.md asks them not to do that.
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := proc.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
	if err != nil {
		// taskkill fails when the process is already gone; make sure
		// the leader at least is dead either way.
		_ = p.Kill()
	}
	return nil
}

// termTree has no graceful equivalent for a windowless process tree on
// Windows, so the kill that follows it is the only signal.
func termTree(*os.Process) {}
