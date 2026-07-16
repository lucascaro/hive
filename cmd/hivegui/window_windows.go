//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func spawnNewGUI(cwd string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self)
	return startDetachedWindows(cmd, cwd)
}

// startDetachedWindows applies the shared detachment + stdio nulling so
// the child process is independent of this (about-to-quit) parent.
// Mirrors startDetached in window_unix.go for the Windows process-detach
// flags. Both spawnNewGUI and spawnHived route through here.
func startDetachedWindows(cmd *exec.Cmd, cwd string) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS
		CreationFlags: 0x00000200 | 0x00000008,
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
