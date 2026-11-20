// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"os"
	"path/filepath"
)

// HandyInstalled reports whether Handy (https://handy.computer) is in a
// standard install location. Settings shows the "start Handy recording"
// switch only when it is (spec 573). The menu bar checks again, and that
// Handy is running, each time the HUD opens.
func (a *App) HandyInstalled() bool {
	paths := []string{"/Applications/Handy.app/Contents/MacOS/handy"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Applications", "Handy.app", "Contents", "MacOS", "handy"))
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
