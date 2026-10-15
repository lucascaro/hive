// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package proc

import "os/exec"

// HideConsole is a no-op off Windows: no other platform invents a
// window for a child process.
func HideConsole(*exec.Cmd) {}
