//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS
		CreationFlags: 0x00000200 | 0x00000008,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
