//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// spawnHived starts a detached `hived` process. It looks up the binary
// using locateHived, then re-execs with setsid so the child outlives
// the GUI.
func spawnHived(sock string, cols, rows int) error {
	bin, err := locateHived()
	if err != nil {
		return err
	}
	args := []string{}
	if sock != "" {
		args = append(args, "--socket", sock)
	}
	if cols > 0 {
		args = append(args, "--cols", fmt.Sprintf("%d", cols))
	}
	if rows > 0 {
		args = append(args, "--rows", fmt.Sprintf("%d", rows))
	}
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
