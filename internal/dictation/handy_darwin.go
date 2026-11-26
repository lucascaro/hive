// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import "os"

func handyPaths() []string {
	home, _ := os.UserHomeDir()
	return darwinHandyPaths(home)
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// handyRunning has no cgo-free answer on macOS: hivebar passes its
// AppKit check to NewExecHandy instead. Without one, Handy counts as not
// running, so Hive never launches it by accident.
func handyRunning() bool { return false }
