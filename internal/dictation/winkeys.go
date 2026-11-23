// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import "github.com/lucascaro/hive/internal/wire"

// The wire carries the dictation chord in macOS terms (wire.GlobalHotkey:
// a kVK_* key code and Carbon modifier bits), so the daemon, the stored
// setting and the GUI's conversion stay one format. The Windows box maps
// it to RegisterHotKey's virtual-key code and MOD_* flags here: plain
// tables, untagged so every platform's tests exercise them.

// Windows RegisterHotKey modifier flags.
const (
	ModAlt      = 0x0001
	ModControl  = 0x0002
	ModShift    = 0x0004
	ModWin      = 0x0008
	ModNoRepeat = 0x4000
)

// kvkToVK maps every kVK the GUI can send (CODE_TO_KVK in
// cmd/hivegui/frontend/src/lib/carbon-keys.ts) to its Windows VK_* code
// on a US layout. Keep the two in step: TestVKForEveryKVK mirrors that
// list.
var kvkToVK = map[uint32]uint32{
	0x00: 'A', 0x0b: 'B', 0x08: 'C', 0x02: 'D', 0x0e: 'E', 0x03: 'F', 0x05: 'G',
	0x04: 'H', 0x22: 'I', 0x26: 'J', 0x28: 'K', 0x25: 'L', 0x2e: 'M', 0x2d: 'N',
	0x1f: 'O', 0x23: 'P', 0x0c: 'Q', 0x0f: 'R', 0x01: 'S', 0x11: 'T', 0x20: 'U',
	0x09: 'V', 0x0d: 'W', 0x07: 'X', 0x10: 'Y', 0x06: 'Z',
	0x1d: '0', 0x12: '1', 0x13: '2', 0x14: '3', 0x15: '4', 0x17: '5', 0x16: '6',
	0x1a: '7', 0x1c: '8', 0x19: '9',
	0x18: 0xBB,                                                             // Equal: VK_OEM_PLUS
	0x1b: 0xBD,                                                             // Minus: VK_OEM_MINUS
	0x21: 0xDB,                                                             // BracketLeft: VK_OEM_4
	0x1e: 0xDD,                                                             // BracketRight: VK_OEM_6
	0x27: 0xDE,                                                             // Quote: VK_OEM_7
	0x29: 0xBA,                                                             // Semicolon: VK_OEM_1
	0x2a: 0xDC,                                                             // Backslash: VK_OEM_5
	0x2b: 0xBC,                                                             // Comma: VK_OEM_COMMA
	0x2f: 0xBE,                                                             // Period: VK_OEM_PERIOD
	0x2c: 0xBF,                                                             // Slash: VK_OEM_2
	0x32: 0xC0,                                                             // Backquote: VK_OEM_3
	0x24: 0x0D,                                                             // Enter: VK_RETURN
	0x30: 0x09,                                                             // Tab: VK_TAB
	0x31: 0x20,                                                             // Space: VK_SPACE
	0x33: 0x08,                                                             // Backspace: VK_BACK
	0x35: 0x1B,                                                             // Escape: VK_ESCAPE
	0x7a: 0x70, 0x78: 0x71, 0x63: 0x72, 0x76: 0x73, 0x60: 0x74, 0x61: 0x75, // F1–F6
	0x62: 0x76, 0x64: 0x77, 0x65: 0x78, 0x6d: 0x79, 0x67: 0x7A, 0x6f: 0x7B, // F7–F12
	0x7b: 0x25, 0x7e: 0x26, 0x7c: 0x27, 0x7d: 0x28, // Left, Up, Right, Down
}

// kvkToScan is the PC set-1 scan code of each punctuation kVK. The GUI
// records punctuation by physical key (`[Minus]`, `[Semicolon]`…: see
// keyToken in lib/keymap-edit.ts), so on Windows it goes through the
// current layout's map and lands on that same physical key.
//
// Letters and digits stay on kvkToVK on purpose. The recorder stores
// them as the character typed (German Y is `Y`), and in-app matching
// compares e.key, while Windows' letter and digit VKs follow the printed
// character on Latin layouts and stay on the US positions on non-Latin
// ones: exactly what was recorded. A scan-code route would move a German
// `Y` chord onto the key labelled Z (review of PR 576).
var kvkToScan = map[uint32]uint32{
	0x18: 0x0D, // Equal
	0x1b: 0x0C, // Minus
	0x1e: 0x1B, // BracketRight
	0x21: 0x1A, // BracketLeft
	0x27: 0x28, // Quote
	0x29: 0x27, // Semicolon
	0x2a: 0x2B, // Backslash
	0x2b: 0x33, // Comma
	0x2c: 0x35, // Slash
	0x2f: 0x34, // Period
	0x32: 0x29, // Backquote
}

// WindowsHotkey is h as RegisterHotKey's (modifiers, vk). ok is false
// for a disabled, invalid or unmappable chord, which registers nothing.
// Cmd maps to the Windows key, Option to Alt. scanToVK is the current
// layout's scan-code-to-VK map (MapVirtualKey on Windows); nil, or a 0
// answer, falls back to the US layout's VK.
func WindowsHotkey(h wire.GlobalHotkey, scanToVK func(scan uint32) uint32) (mods, vk uint32, ok bool) {
	if !h.Enabled || !h.Valid() {
		return 0, 0, false
	}
	vk, ok = kvkToVK[h.KeyCode]
	if !ok {
		return 0, 0, false
	}
	if scan, layoutDependent := kvkToScan[h.KeyCode]; layoutDependent && scanToVK != nil {
		if v := scanToVK(scan); v != 0 {
			vk = v
		}
	}
	if h.Modifiers&wire.HotkeyModControl != 0 {
		mods |= ModControl
	}
	if h.Modifiers&wire.HotkeyModOption != 0 {
		mods |= ModAlt
	}
	if h.Modifiers&wire.HotkeyModShift != 0 {
		mods |= ModShift
	}
	if h.Modifiers&wire.HotkeyModCmd != 0 {
		mods |= ModWin
	}
	return mods | ModNoRepeat, vk, true
}

// EditAction is what a key in the Windows box's text field does.
type EditAction int

const (
	EditDefault EditAction = iota // let the field handle it
	EditSubmit
	EditNewLine
	EditCancel
	EditPrev
	EditNext
)

// Windows virtual-key codes the box's field reacts to.
const (
	vkReturn = 0x0D
	vkEscape = 0x1B
	vkUp     = 0x26
	vkDown   = 0x28
)

// EditKeyAction maps a key press in the box's field to what it does.
// The keys match the macOS box (spec 553): Enter sends, Shift+Enter
// starts a new line, Esc cancels and the arrows change the target.
func EditKeyAction(vk uint32, shift, ctrl, alt bool) EditAction {
	plain := !shift && !ctrl && !alt
	switch {
	case vk == vkReturn && plain:
		return EditSubmit
	case vk == vkReturn && shift && !ctrl && !alt:
		return EditNewLine
	case vk == vkEscape && plain:
		return EditCancel
	case vk == vkUp && plain:
		return EditPrev
	case vk == vkDown && plain:
		return EditNext
	}
	return EditDefault
}
