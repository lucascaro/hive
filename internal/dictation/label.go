// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

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
