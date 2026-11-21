// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package wire

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// TestHUDFramesCarryNoText pins spec 553's rule that the capture HUD adds
// no new way to put text into a session: the HUD's own frames carry a
// session id, a key chord and switches, nothing else. Text goes through
// SEND_TO_SESSION. A string field added anywhere in a payload, nested
// or behind a pointer, fails here.
func TestHUDFramesCarryNoText(t *testing.T) {
	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			ft, name := f.Type, path+"."+f.Name
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			switch k := ft.Kind(); {
			case k == reflect.String && f.Tag.Get("json") != "session_id":
				t.Errorf("%s is a string field; HUD frames must not carry text", name)
			case k == reflect.Struct:
				walk(ft, name)
			case k == reflect.Slice || k == reflect.Map || k == reflect.Interface:
				t.Errorf("%s (%s) could carry text", name, k)
			}
		}
	}
	for _, v := range []any{SetFocusedSessionReq{}, Dictation{}, SetDictationReq{}} {
		walk(reflect.TypeOf(v), reflect.TypeOf(v).Name())
	}
}

// TestDictationJSON pins the wire shape: snake_case keys, and a
// SET_DICTATION that names one setting omits the other.
func TestDictationJSON(t *testing.T) {
	b, _ := json.Marshal(DefaultDictation)
	if got := string(b); got != `{"hotkey":{"key_code":49,"modifiers":6144,"enabled":true},"handy_auto_start":false}` {
		t.Errorf("Dictation = %s", got)
	}
	on := true
	b, _ = json.Marshal(SetDictationReq{HandyAutoStart: &on})
	if got := string(b); got != `{"handy_auto_start":true}` {
		t.Errorf("SetDictationReq = %s", got)
	}
}

func TestSetDictationReqValidAndApply(t *testing.T) {
	on, bad := true, GlobalHotkey{Enabled: true, KeyCode: 49}
	if (SetDictationReq{}).Valid() {
		t.Error("an empty request is valid; it must name a setting")
	}
	if (SetDictationReq{Hotkey: &bad}).Valid() {
		t.Error("a request with an unregistrable hotkey is valid")
	}
	if !(SetDictationReq{HandyAutoStart: &on}).Valid() {
		t.Error("a switch-only request is invalid")
	}
	d := SetDictationReq{HandyAutoStart: &on}.Apply(DefaultDictation)
	if d.Hotkey != DefaultGlobalHotkey || !d.HandyAutoStart {
		t.Errorf("switch-only Apply = %+v; the hotkey must be untouched", d)
	}
	h := GlobalHotkey{KeyCode: 2, Modifiers: HotkeyModCmd, Enabled: true}
	d = SetDictationReq{Hotkey: &h}.Apply(Dictation{Hotkey: DefaultGlobalHotkey, HandyAutoStart: true})
	if d.Hotkey != h || !d.HandyAutoStart {
		t.Errorf("hotkey-only Apply = %+v; the switch must be untouched", d)
	}
}

// TestRetiredHotkeyFramesStayRetired: 0x58/0x59 carried the hotkey-only
// payload a still-running older menu bar decodes. They must never come
// back with a new shape, and the dictation frames are classified.
func TestRetiredHotkeyFramesStayRetired(t *testing.T) {
	for _, list := range [][]FrameType{ControlRequestFrames, ControlEventFrames, NonControlFrames} {
		for _, f := range list {
			if f == 0x58 || f == 0x59 {
				t.Errorf("frame %#x is retired but listed", uint8(f))
			}
		}
	}
	if !slices.Contains(ControlRequestFrames, FrameSetDictation) || !slices.Contains(ControlEventFrames, FrameDictation) {
		t.Error("SET_DICTATION / DICTATION are not classified")
	}
}

func TestGlobalHotkeyValid(t *testing.T) {
	cases := []struct {
		h    GlobalHotkey
		want bool
	}{
		{DefaultGlobalHotkey, true},
		{GlobalHotkey{Enabled: false, KeyCode: 999, Modifiers: 0xffff}, true},
		{GlobalHotkey{Enabled: true, KeyCode: 49}, false},
		{GlobalHotkey{Enabled: true, KeyCode: 128, Modifiers: HotkeyModCmd}, false},
		{GlobalHotkey{Enabled: true, KeyCode: 0, Modifiers: HotkeyModCmd | 0x0001}, false},
	}
	for _, c := range cases {
		if got := c.h.Valid(); got != c.want {
			t.Errorf("%+v.Valid() = %v, want %v", c.h, got, c.want)
		}
	}
}
