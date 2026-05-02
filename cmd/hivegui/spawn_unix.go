//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// spawnHived starts a detached `hived` process. cwd, if non-empty,
// becomes both the daemon's working directory (cmd.Dir) and is
// forwarded as --cwd so the daemon explicitly knows the user's
// original launch directory regardless of what macOS did to the .app
// process's cwd.
func spawnHived(sock, cwd string) error {
	bin, err := locateHived()
	if err != nil {
		return err
	}
	args := []string{}
	if sock != "" {
		args = append(args, "--socket", sock)
	}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
