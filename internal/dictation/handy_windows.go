// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func handyPaths() []string { return windowsHandyPaths(os.Getenv) }

// isExecutable: Windows has no execute bit; a regular file is enough.
func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// handyRunning reports a handy.exe process in this session's process
// list. Cheap enough for the HUD's Open: one Toolhelp snapshot.
func handyRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "handy.exe") {
			return true
		}
	}
	return false
}
