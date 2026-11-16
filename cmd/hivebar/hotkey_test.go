// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

type fakeRegistrar struct{ got []wire.GlobalHotkey }

func (r *fakeRegistrar) Register(h wire.GlobalHotkey) { r.got = append(r.got, h) }

type menuLine struct {
	label   string
	enabled bool
}

func newTestDictation() (*Dictation, *fakeRegistrar, *[]menuLine) {
	r := &fakeRegistrar{}
	var lines []menuLine
	d := NewDictation(r, func(l string, on bool) { lines = append(lines, menuLine{l, on}) })
	return d, r, &lines
}

func lastLine(lines *[]menuLine) menuLine { return (*lines)[len(*lines)-1] }

func TestDictationRegistersDaemonChord(t *testing.T) {
	d, r, lines := newTestDictation()
	d.Connected(hudContract)
	d.SetHotkey(wire.DefaultGlobalHotkey)
	if len(r.got) != 1 || r.got[0] != wire.DefaultGlobalHotkey {
		t.Fatalf("registered %+v, want the default once", r.got)
	}
	d.Registered(wire.DefaultGlobalHotkey, true)
	if l := lastLine(lines); !l.enabled || !strings.HasSuffix(l.label, "⌃⌥Space") {
		t.Fatalf("menu line = %+v", l)
	}
}

// A reconnect re-delivers the same chord: registering it again would be
// harmless (the native side unregisters first), but nothing needs to.
func TestDictationReconnectSameChordNoReregister(t *testing.T) {
	d, r, _ := newTestDictation()
	d.Connected(hudContract)
	d.SetHotkey(wire.DefaultGlobalHotkey)
	d.Disconnected()
	d.Connected(hudContract)
	d.SetHotkey(wire.DefaultGlobalHotkey)
	if len(r.got) != 1 {
		t.Fatalf("registered %d times, want 1", len(r.got))
	}
	next := wire.GlobalHotkey{KeyCode: 2, Modifiers: wire.HotkeyModCmd | wire.HotkeyModShift, Enabled: true}
	d.SetHotkey(next)
	if len(r.got) != 2 || r.got[1] != next {
		t.Fatalf("a changed chord was not registered: %+v", r.got)
	}
}

func TestDictationTakenChordSaysSo(t *testing.T) {
	d, _, lines := newTestDictation()
	d.Connected(hudContract)
	d.SetHotkey(wire.DefaultGlobalHotkey)
	d.Registered(wire.DefaultGlobalHotkey, false)
	if l := lastLine(lines); !strings.Contains(l.label, "taken by another app") || !l.enabled {
		t.Fatalf("menu line = %+v", l)
	}
	// An answer for a chord since replaced is ignored.
	next := wire.GlobalHotkey{KeyCode: 2, Modifiers: wire.HotkeyModCmd, Enabled: true}
	d.SetHotkey(next)
	d.Registered(wire.DefaultGlobalHotkey, false)
	if l := lastLine(lines); strings.Contains(l.label, "taken") {
		t.Fatalf("a stale refusal marked the new chord taken: %+v", l)
	}
}

func TestDictationOldDaemonShowsRestartHint(t *testing.T) {
	d, r, lines := newTestDictation()
	d.Connected(hudContract - 1)
	if l := lastLine(lines); l.enabled || !strings.Contains(l.label, "restart") {
		t.Fatalf("menu line on an old daemon = %+v", l)
	}
	if len(r.got) != 0 {
		t.Fatal("registered a hotkey no daemon relayed")
	}
}

func TestDictationDisabledUnregisters(t *testing.T) {
	d, r, lines := newTestDictation()
	d.Connected(hudContract)
	d.SetHotkey(wire.DefaultGlobalHotkey)
	d.SetHotkey(wire.GlobalHotkey{Enabled: false})
	if len(r.got) != 2 || r.got[1].Enabled {
		t.Fatalf("disable did not unregister: %+v", r.got)
	}
	if l := lastLine(lines); l.label != dictateTitle || !l.enabled {
		t.Fatalf("menu line with no hotkey = %+v", l)
	}
}

func TestHotkeyLabel(t *testing.T) {
	for _, c := range []struct {
		h    wire.GlobalHotkey
		want string
	}{
		{wire.DefaultGlobalHotkey, "⌃⌥Space"},
		{wire.GlobalHotkey{KeyCode: 2, Modifiers: wire.HotkeyModCmd | wire.HotkeyModShift}, "⇧⌘D"},
		{wire.GlobalHotkey{KeyCode: 0x7a, Modifiers: wire.HotkeyModControl}, "⌃F1"},
		{wire.GlobalHotkey{KeyCode: 0x7f, Modifiers: wire.HotkeyModCmd}, "⌘key 127"},
	} {
		if got := hotkeyLabel(c.h); got != c.want {
			t.Errorf("hotkeyLabel(%+v) = %q, want %q", c.h, got, c.want)
		}
	}
}
