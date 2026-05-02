//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

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
