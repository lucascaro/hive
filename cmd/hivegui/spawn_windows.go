//go:build windows

package main

import (
	"os/exec"
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
	return startDetachedWindows(cmd, cwd)
}
