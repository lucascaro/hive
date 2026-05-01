//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// spawnNewGUI launches another instance of the GUI binary as a
// detached child so the two processes have independent windows. We
// re-exec ourselves rather than `open -n /path/to/Hive.app` because
// the user might have launched the binary directly during dev; both
// paths land at the same .app/Contents/MacOS/hivegui executable.
func spawnNewGUI(cwd string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
