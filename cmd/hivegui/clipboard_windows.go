// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// Standard clipboard formats (winuser.h). x/sys/windows does not name them.
const (
	cfBitmap      = 2
	cfDIB         = 8
	cfUnicodeText = 13
	cfDIBV5       = 17
)

var procIsClipboardFormatAvailable = windows.NewLazySystemDLL("user32.dll").NewProc("IsClipboardFormatAvailable")

// clipboardHas needs no OpenClipboard, so it has none of the STA /
// thread-affinity trouble the read and write calls do.
func clipboardHas(format uintptr) bool {
	r, _, _ := procIsClipboardFormatAvailable.Call(format)
	return r != 0
}

func readClipboard(a *App) (ClipboardContents, error) {
	return pickClipboard(
		clipboardHas(cfUnicodeText),
		clipboardHas(cfDIB) || clipboardHas(cfDIBV5) || clipboardHas(cfBitmap),
		func() (string, error) { return wruntime.ClipboardGetText(a.ctx) },
	)
}
