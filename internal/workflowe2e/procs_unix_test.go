// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package workflowe2e

import "syscall"

// alive reports whether pid names a running process.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// kill9 sends SIGKILL to pid: a crash, with no cleanup.
func kill9(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }
