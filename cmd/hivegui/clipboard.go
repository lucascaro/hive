// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

// ClipboardContents is what the terminal's paste chord (Ctrl+Shift+V)
// reads. Image is set only when the clipboard holds an image and no
// text: the terminal cannot paste image bytes, so it forwards the
// agent's own image-paste key instead and lets the agent read the
// clipboard itself.
type ClipboardContents struct {
	Text  string `json:"text"`
	Image bool   `json:"image"`
}

// ReadClipboard replaces wails runtime.ClipboardGetText for the paste
// chord. On Windows that call fails when the clipboard holds no text
// (an image, say), and the error it returns is ERROR_SUCCESS — so the
// user saw "paste failed: The operation completed successfully."
func (a *App) ReadClipboard() (ClipboardContents, error) {
	return readClipboard(a)
}

// pickClipboard is the platform-neutral decision behind readClipboard:
// read text only when there is some, otherwise report whether an image
// is waiting. Split out so it is testable off Windows.
func pickClipboard(hasText, hasImage bool, readText func() (string, error)) (ClipboardContents, error) {
	if !hasText {
		return ClipboardContents{Image: hasImage}, nil
	}
	text, err := readText()
	return ClipboardContents{Text: text}, err
}
