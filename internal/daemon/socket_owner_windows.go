// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build windows

package daemon

import "os"

// dirOwnerUID is unreachable on Windows: CheckSocketDir returns before
// it. It exists so the POSIX-only syscall.Stat_t stays out of the
// Windows build.
func dirOwnerUID(os.FileInfo) (int, error) { return 0, errNoOwner }
