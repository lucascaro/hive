// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package main

import wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

// readClipboard: off Windows the runtime returns "" (no error) for a
// text-less clipboard, so there is nothing to work around. Image stays
// false — those agents take image paste on Ctrl+V, which the terminal
// already forwards untouched.
func readClipboard(a *App) (ClipboardContents, error) {
	text, err := wruntime.ClipboardGetText(a.ctx)
	return ClipboardContents{Text: text}, err
}
