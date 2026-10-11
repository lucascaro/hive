// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build windows

package main

// installSignalDiag is a no-op on Windows: SIGHUP/SIGTERM/SIGINT don't
// exist there in the POSIX sense, and Wails' own signal handling (see
// signal_diag_unix.go) is unix-only too.
func installSignalDiag() {}
