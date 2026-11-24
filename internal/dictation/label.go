// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import "strings"

// Label names a live session the way the menu bar and the dictation box
// show it: "project · name — title". name falls back to the id, and the
// title is left out when it only repeats the name.
func Label(project, id, name, title string) string {
	if name == "" {
		name = id
	}
	label := name
	if project != "" {
		label = project + " · " + name
	}
	if title != "" && title != name {
		label += " — " + title
	}
	return label
}

// FromEdit turns a Windows edit control's text into the box's: the
// control ends lines with CRLF, and the HUD counts runes to tell
// Handy's transcription (two or more at once) from a key press, so a
// Shift+Enter must arrive as one rune, not two.
func FromEdit(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
