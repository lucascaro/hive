// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework Carbon
#include <stdlib.h>
#include "hud_darwin.h"
*/
import "C"

import (
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/lucascaro/hive/internal/wire"
)

// The Go side of hud_darwin.m. The panel and hotkey call back into the
// functions exported below on the main thread; each forwards to the HUD
// or the Dictation, whose methods only take a short lock and return.

// nativeTargets are the receivers of the exported callbacks, set once in
// main before anything can fire.
var nativeTargets atomic.Pointer[struct {
	hud  *HUD
	dict *Dictation
}]

func setNativeTargets(h *HUD, d *Dictation) {
	nativeTargets.Store(&struct {
		hud  *HUD
		dict *Dictation
	}{h, d})
}

// nativeView is the AppKit panel.
type nativeView struct{}

func (nativeView) Show(st hudState, clear, focus bool) {
	labels := make([]string, len(st.Targets))
	for i, t := range st.Targets {
		// \x1f separates labels on the way into Objective-C; a title is
		// whatever the child process set, so it must not carry one.
		labels[i] = strings.Map(func(r rune) rune {
			if r < 0x20 {
				return ' '
			}
			return r
		}, t.Label)
	}
	cl := C.CString(strings.Join(labels, "\x1f"))
	cs := C.CString(st.Status)
	ch := C.CString(st.Hints)
	defer C.free(unsafe.Pointer(cl))
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(ch))
	clr := C.int(0)
	if clear {
		clr = 1
	}
	foc := C.int(0)
	if focus {
		foc = 1
	}
	C.hbHUDShow(cl, C.int(len(labels)), C.int(st.Selected), cs, ch, clr, foc)
}

func (nativeView) Hide(restore bool) {
	r := C.int(0)
	if restore {
		r = 1
	}
	C.hbHUDHide(r)
}

// nativeRegistrar registers the hotkey with Carbon.
type nativeRegistrar struct{}

func (nativeRegistrar) Register(h wire.GlobalHotkey) {
	en := C.int(0)
	if h.Enabled {
		en = 1
	}
	C.hbHotkeySet(C.uint32_t(h.KeyCode), C.uint32_t(h.Modifiers), en)
}

//export goHotkeyPressed
func goHotkeyPressed() {
	if t := nativeTargets.Load(); t != nil {
		t.hud.Open()
	}
}

//export goHotkeyRegistered
func goHotkeyRegistered(keyCode, modifiers C.uint32_t, enabled, ok C.int) {
	if t := nativeTargets.Load(); t != nil {
		h := wire.GlobalHotkey{KeyCode: uint32(keyCode), Modifiers: uint32(modifiers), Enabled: enabled != 0}
		if !h.Enabled {
			h.KeyCode, h.Modifiers = 0, 0
		}
		t.dict.Registered(h, ok != 0)
	}
}

//export goHUDSubmit
func goHUDSubmit(text *C.char) {
	if t := nativeTargets.Load(); t != nil {
		t.hud.Submit(C.GoString(text))
	}
}

//export goHUDTextChanged
func goHUDTextChanged(text *C.char, pasted C.int) {
	if t := nativeTargets.Load(); t != nil {
		t.hud.TextChanged(C.GoString(text), pasted != 0)
	}
}

// nativeHandyRunning reports that Handy is running, from AppKit's list of
// running applications: cheap, and safe to call on any thread.
func nativeHandyRunning() bool {
	id := C.CString(handyBundleID)
	defer C.free(unsafe.Pointer(id))
	return C.hbAppRunning(id) != 0
}

//export goHUDCancel
func goHUDCancel() {
	if t := nativeTargets.Load(); t != nil {
		t.hud.Cancel()
	}
}

//export goHUDMove
func goHUDMove(delta C.int) {
	if t := nativeTargets.Load(); t != nil {
		t.hud.Move(int(delta))
	}
}

//export goHUDSelect
func goHUDSelect(index C.int) {
	if t := nativeTargets.Load(); t != nil {
		t.hud.Select(int(index))
	}
}
