// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"fmt"
	"sync"

	"github.com/lucascaro/hive/internal/wire"
)

// The system-wide hotkey that opens the dictation HUD (spec 553).
//
// The chord is the user's keymap entry for `dictate-hud`, which the GUI
// converts and the daemon relays in DICTATION. hivebar registers it
// with Carbon's RegisterEventHotKey, which needs no Accessibility or
// Input Monitoring permission (unlike an event tap, or an NSEvent global
// monitor) and swallows the chord before any app sees it.
//
// Dictation decides what to register and what the menu says about it.
// The Carbon calls themselves are hud_darwin.m's, behind registrar.

// registrar registers a hotkey with the OS. Register replaces whatever
// was registered before (so a reconnect that re-delivers the same chord
// is harmless) and reports the outcome later through
// Dictation.Registered. A disabled hotkey only unregisters.
type registrar interface {
	Register(h wire.GlobalHotkey)
}

// Dictation tracks the hotkey and renders its menu line.
type Dictation struct {
	reg registrar
	// onChange receives the menu line and whether clicking it opens the
	// HUD. Called with the lock held; it must not block.
	onChange func(label string, enabled bool)

	mu        sync.Mutex
	connected bool
	contract  int
	want      wire.GlobalHotkey
	have      bool // a DICTATION has arrived since connecting
	taken     bool // the OS refused the chord: another app holds it
	sent      wire.GlobalHotkey
	sentValid bool
}

func NewDictation(reg registrar, onChange func(string, bool)) *Dictation {
	return &Dictation{reg: reg, onChange: onChange}
}

// Connected records a daemon handshake and its contract.
func (d *Dictation) Connected(contract int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected, d.contract, d.have = true, contract, false
	d.renderLocked()
}

// Disconnected records that the daemon went away. The registration is
// kept: the HUD still opens, and says the daemon is not running if the
// user tries to send.
func (d *Dictation) Disconnected() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = false
	d.renderLocked()
}

// SetHotkey is the chord in a DICTATION from the daemon.
func (d *Dictation) SetHotkey(h wire.GlobalHotkey) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !h.Valid() {
		h = wire.GlobalHotkey{}
	}
	if !h.Enabled {
		h.KeyCode, h.Modifiers = 0, 0
	}
	d.want, d.have = h, true
	if d.sentValid && d.sent == h {
		d.renderLocked()
		return
	}
	d.sent, d.sentValid, d.taken = h, true, false
	d.reg.Register(h)
	d.renderLocked()
}

// Registered is the OS's answer to the last Register: ok false means the
// chord is already registered by another app.
func (d *Dictation) Registered(h wire.GlobalHotkey, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if h != d.sent {
		return // an answer to a chord since replaced
	}
	d.taken = !ok
	d.renderLocked()
}

const dictateTitle = "Dictate into a Session…"

func (d *Dictation) renderLocked() {
	if d.onChange == nil {
		return
	}
	d.onChange(d.labelLocked())
}

func (d *Dictation) labelLocked() (string, bool) {
	switch {
	case d.connected && d.contract < hudContract:
		return "Dictation needs a newer daemon — restart it", false
	case !d.sentValid || !d.want.Enabled:
		return dictateTitle, true
	case d.taken:
		return fmt.Sprintf("%s (%s is taken by another app)", dictateTitle, hotkeyLabel(d.want)), true
	default:
		return fmt.Sprintf("%s  %s", dictateTitle, hotkeyLabel(d.want)), true
	}
}

// kVK → the key's name on the keycap, for the menu line.
var keyNames = map[uint32]string{
	0x00: "A", 0x01: "S", 0x02: "D", 0x03: "F", 0x04: "H", 0x05: "G", 0x06: "Z", 0x07: "X",
	0x08: "C", 0x09: "V", 0x0b: "B", 0x0c: "Q", 0x0d: "W", 0x0e: "E", 0x0f: "R", 0x10: "Y",
	0x11: "T", 0x12: "1", 0x13: "2", 0x14: "3", 0x15: "4", 0x16: "6", 0x17: "5", 0x18: "=",
	0x19: "9", 0x1a: "7", 0x1b: "-", 0x1c: "8", 0x1d: "0", 0x1e: "]", 0x1f: "O", 0x20: "U",
	0x21: "[", 0x22: "I", 0x23: "P", 0x24: "↩", 0x25: "L", 0x26: "J", 0x27: "'", 0x28: "K",
	0x29: ";", 0x2a: "\\", 0x2b: ",", 0x2c: "/", 0x2d: "N", 0x2e: "M", 0x2f: ".", 0x30: "⇥",
	0x31: "Space", 0x32: "`", 0x33: "⌫", 0x35: "⎋",
	0x60: "F5", 0x61: "F6", 0x62: "F7", 0x63: "F3", 0x64: "F8", 0x65: "F9", 0x67: "F11",
	0x6d: "F10", 0x6f: "F12", 0x76: "F4", 0x78: "F2", 0x7a: "F1",
	0x7b: "←", 0x7c: "→", 0x7d: "↓", 0x7e: "↑",
}

// hotkeyLabel renders a chord the way macOS menus do: ⌃⌥⇧⌘ then the key.
func hotkeyLabel(h wire.GlobalHotkey) string {
	s := ""
	for _, m := range []struct {
		bit   uint32
		glyph string
	}{
		{wire.HotkeyModControl, "⌃"},
		{wire.HotkeyModOption, "⌥"},
		{wire.HotkeyModShift, "⇧"},
		{wire.HotkeyModCmd, "⌘"},
	} {
		if h.Modifiers&m.bit != 0 {
			s += m.glyph
		}
	}
	name, ok := keyNames[h.KeyCode]
	if !ok {
		name = fmt.Sprintf("key %d", h.KeyCode)
	}
	return s + name
}
