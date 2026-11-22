// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// fakeOS is the session's hotkey table: one holder per chord.
type fakeOS struct {
	holder map[wire.GlobalHotkey]*Owner
}

func (f *fakeOS) owner() (o *Owner) {
	var mine wire.GlobalHotkey
	o = NewOwner(
		func(h wire.GlobalHotkey) bool {
			if f.holder[h] != nil {
				return false
			}
			f.holder[h], mine = o, h
			return true
		},
		func() { delete(f.holder, mine) },
		nil,
	)
	return o
}

var chordA = wire.GlobalHotkey{KeyCode: 0x31, Modifiers: wire.HotkeyModControl | wire.HotkeyModShift, Enabled: true}
var chordB = wire.GlobalHotkey{KeyCode: 0x31, Modifiers: wire.HotkeyModControl | wire.HotkeyModOption, Enabled: true}

func TestOwnerRetriesWhileLosing(t *testing.T) {
	sys := &fakeOS{holder: map[wire.GlobalHotkey]*Owner{}}
	a, b := sys.owner(), sys.owner()
	a.Want(chordA)
	b.Want(chordA)
	if !a.Held() || b.Held() || !b.Retrying() {
		t.Fatalf("a held %v, b held %v retrying %v; want exactly a", a.Held(), b.Held(), b.Retrying())
	}
	b.Tick()
	if b.Held() {
		t.Fatal("b took a chord a still holds")
	}
}

func TestOwnerTakesOverWhenFree(t *testing.T) {
	sys := &fakeOS{holder: map[wire.GlobalHotkey]*Owner{}}
	a, b := sys.owner(), sys.owner()
	a.Want(chordA)
	b.Want(chordA)
	a.Release() // the owning window closed
	b.Tick()
	if !b.Held() || b.Retrying() {
		t.Fatalf("b held %v retrying %v after a let go", b.Held(), b.Retrying())
	}
}

func TestOwnerUnregistersOnDisable(t *testing.T) {
	sys := &fakeOS{holder: map[wire.GlobalHotkey]*Owner{}}
	a := sys.owner()
	a.Want(chordA)
	a.Want(wire.GlobalHotkey{})
	if a.Held() || a.Retrying() || len(sys.holder) != 0 {
		t.Fatalf("disabled chord still held (%v) or retried (%v)", a.Held(), a.Retrying())
	}
}

func TestOwnerChordChangeWhileLosing(t *testing.T) {
	sys := &fakeOS{holder: map[wire.GlobalHotkey]*Owner{}}
	a, b := sys.owner(), sys.owner()
	a.Want(chordA)
	b.Want(chordA)
	// The user rebinds: the owner moves first, the loser follows.
	a.Want(chordB)
	b.Want(chordB)
	if !a.Held() || b.Held() || !b.Retrying() {
		t.Fatalf("after a rebind: a held %v, b held %v retrying %v", a.Held(), b.Held(), b.Retrying())
	}
	if sys.holder[chordA] != nil {
		t.Fatal("the old chord stayed registered")
	}
	a.Release()
	b.Tick()
	if !b.Held() || sys.holder[chordB] != b {
		t.Fatal("b did not take the new chord once free")
	}
}
