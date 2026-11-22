// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// Every kVK in CODE_TO_KVK (cmd/hivegui/frontend/src/lib/carbon-keys.ts),
// copied here: a chord the GUI can send must register on Windows.
var guiKVKs = []uint32{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e,
	0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d,
	0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x35, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x67, 0x6d,
	0x6f, 0x76, 0x78, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e,
}

func TestVKForEveryKVK(t *testing.T) {
	seen := map[uint32]uint32{}
	for _, k := range guiKVKs {
		vk, ok := kvkToVK[k]
		if !ok || vk == 0 {
			t.Errorf("kVK %#x has no Windows VK", k)
			continue
		}
		if other, dup := seen[vk]; dup {
			t.Errorf("kVK %#x and %#x both map to VK %#x", k, other, vk)
		}
		seen[vk] = k
	}
	if len(kvkToVK) != len(guiKVKs) {
		t.Errorf("table has %d keys, the GUI sends %d", len(kvkToVK), len(guiKVKs))
	}
}

func TestWindowsModsFromCarbon(t *testing.T) {
	space := uint32(0x31)
	for _, c := range []struct {
		carbon, mods uint32
	}{
		{wire.HotkeyModControl | wire.HotkeyModShift, ModControl | ModShift},
		{wire.HotkeyModControl | wire.HotkeyModOption, ModControl | ModAlt},
		{wire.HotkeyModCmd, ModWin},
	} {
		mods, vk, ok := WindowsHotkey(wire.GlobalHotkey{KeyCode: space, Modifiers: c.carbon, Enabled: true})
		if !ok || vk != 0x20 || mods != c.mods|ModNoRepeat {
			t.Errorf("carbon %#x: mods %#x vk %#x ok %v, want mods %#x vk 0x20", c.carbon, mods, vk, ok, c.mods|ModNoRepeat)
		}
	}
	for name, h := range map[string]wire.GlobalHotkey{
		"disabled":     {KeyCode: space, Modifiers: wire.HotkeyModControl},
		"no modifier":  {KeyCode: space, Enabled: true},
		"unknown bit":  {KeyCode: space, Modifiers: 0x0001, Enabled: true},
		"unmapped key": {KeyCode: 0x34, Modifiers: wire.HotkeyModControl, Enabled: true},
	} {
		if _, _, ok := WindowsHotkey(h); ok {
			t.Errorf("%s: registered", name)
		}
	}
}

func TestEditKeyAction(t *testing.T) {
	for _, c := range []struct {
		vk               uint32
		shift, ctrl, alt bool
		want             EditAction
	}{
		{vkReturn, false, false, false, EditSubmit},
		{vkReturn, true, false, false, EditNewLine},
		{vkReturn, false, true, false, EditDefault},
		{vkEscape, false, false, false, EditCancel},
		{vkUp, false, false, false, EditPrev},
		{vkDown, false, false, false, EditNext},
		{vkDown, true, false, false, EditDefault},
		{'A', false, false, false, EditDefault},
	} {
		if got := EditKeyAction(c.vk, c.shift, c.ctrl, c.alt); got != c.want {
			t.Errorf("vk %#x shift=%v ctrl=%v alt=%v: %v, want %v", c.vk, c.shift, c.ctrl, c.alt, got, c.want)
		}
	}
}
