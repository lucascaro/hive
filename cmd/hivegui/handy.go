// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import "github.com/lucascaro/hive/internal/dictation"

// HandyInstalled reports whether Handy (https://handy.computer) is in a
// standard install location for this platform. Settings shows the
// "start Handy recording" switch only when it is (specs 573, 575). The
// dictation box checks again, and that Handy is running, each time it
// opens. False where there is no dictation box.
func (a *App) HandyInstalled() bool { return dictation.HandyBin() != "" }
