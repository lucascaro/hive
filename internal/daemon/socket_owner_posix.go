// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// dirOwnerUID reads the owning uid out of a POSIX stat result.
func dirOwnerUID(st os.FileInfo) (int, error) {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errNoOwner
	}
	return int(sys.Uid), nil
}
