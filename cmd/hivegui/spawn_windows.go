// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

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
