// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package workflowe2e

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// alive reports whether pid names a running process. A killed process
// that nobody has reaped yet (a zombie: an orphan in a container without
// an init) counts as dead.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		s := string(b)
		if i := strings.LastIndex(s, ")"); i >= 0 && len(s) > i+2 && s[i+2] == 'Z' {
			return false
		}
	}
	return true
}

// kill9 sends SIGKILL to pid: a crash, with no cleanup.
func kill9(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }
