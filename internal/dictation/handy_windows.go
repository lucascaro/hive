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

// handyRunning reports a handy.exe process in this Windows session. The
// Toolhelp snapshot lists every session's processes, and another login's
// Handy would not answer this one's flags. Cheap enough for the HUD's
// Open: one snapshot.
func handyRunning() bool {
	var mine uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &mine) != nil {
		return false
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "handy.exe") {
			continue
		}
		var sid uint32
		if windows.ProcessIdToSessionId(e.ProcessID, &sid) == nil && sid == mine {
			return true
		}
	}
	return false
}
