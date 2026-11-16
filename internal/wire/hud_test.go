// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package wire

import (
	"reflect"
	"testing"
)

// TestHUDFramesCarryNoText pins spec 553's rule that the capture HUD adds
// no new way to put text into a session: the HUD's own frames carry a
// session id and a key chord, nothing else. Text goes through
// SEND_TO_SESSION. A string field added to either payload fails here.
func TestHUDFramesCarryNoText(t *testing.T) {
	for _, v := range []any{SetFocusedSessionReq{}, GlobalHotkey{}} {
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Type.Kind() == reflect.String && f.Tag.Get("json") != "session_id" {
				t.Errorf("%s.%s is a string field; HUD frames must not carry text", rt.Name(), f.Name)
			}
			if k := f.Type.Kind(); k == reflect.Slice || k == reflect.Map || k == reflect.Interface || k == reflect.Struct {
				t.Errorf("%s.%s (%s) could carry text", rt.Name(), f.Name, k)
			}
		}
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
