// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package main

// The dictation box is hosted by the GUI only on Windows (spec 575). On
// macOS the menu bar (cmd/hivebar) hosts it; elsewhere there is none.
func (a *App) startDictation() {}
func (a *App) stopDictation()  {}
