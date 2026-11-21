// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func expectRefused(t *testing.T, errs chan wire.Error) {
	t.Helper()
	select {
	case e := <-errs:
		if e.Code != wire.ErrCodeModeNotAllowed {
			t.Fatalf("code = %q, want %q", e.Code, wire.ErrCodeModeNotAllowed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no refusal")
	}
}

// A plugin able to move the newest focus could steer which session the
// user's dictated text lands in.
func TestSetFocusedSessionRefusesPlugin(t *testing.T) {
	d := newFrameTestDaemon(t)
	id := d.reg.List()[0].ID
	ops, errs, _ := asyncOps("plugin:acme")
	d.handleControlFrame(context.Background(), ops, wire.FrameSetFocusedSession, mustJSON(t, wire.SetFocusedSessionReq{SessionID: id}))
	expectRefused(t, errs)
	for _, s := range d.reg.List() {
		if s.FocusedAt != 0 {
			t.Fatalf("a refused report stamped %s", s.ID)
		}
	}
}

func TestSetFocusedSessionStamps(t *testing.T) {
	d := newFrameTestDaemon(t)
	id := d.reg.List()[0].ID
	ops, errs, _ := asyncOps("")
	d.handleControlFrame(context.Background(), ops, wire.FrameSetFocusedSession, mustJSON(t, wire.SetFocusedSessionReq{SessionID: id}))
	select {
	case e := <-errs:
		t.Fatalf("unexpected error %+v", e)
	default:
	}
	if d.reg.List()[0].FocusedAt == 0 {
		t.Fatal("focus not stamped")
	}
	d.handleControlFrame(context.Background(), ops, wire.FrameSetFocusedSession, mustJSON(t, wire.SetFocusedSessionReq{SessionID: "nope"}))
	if e := <-errs; e.Code != "no_such_session" {
		t.Fatalf("unknown session code = %q", e.Code)
	}
}

func TestSetDictationRefusesPlugin(t *testing.T) {
	d := newFrameTestDaemon(t)
	ops, errs, _ := asyncOps("plugin:acme")
	on := true
	d.handleControlFrame(context.Background(), ops, wire.FrameSetDictation, mustJSON(t, wire.SetDictationReq{HandyAutoStart: &on}))
	expectRefused(t, errs)
	if got := d.reg.Dictation(); got != wire.DefaultDictation {
		t.Fatalf("a plugin changed the dictation settings to %+v", got)
	}
}

func TestSetDictationRejectsInvalid(t *testing.T) {
	for name, req := range map[string]wire.SetDictationReq{
		"empty":      {},
		"bad hotkey": {Hotkey: &wire.GlobalHotkey{KeyCode: 49, Enabled: true}},
	} {
		t.Run(name, func(t *testing.T) {
			d := newFrameTestDaemon(t)
			ops, errs, _ := asyncOps("")
			d.handleControlFrame(context.Background(), ops, wire.FrameSetDictation, mustJSON(t, req))
			if e := <-errs; e.Code != "bad_payload" {
				t.Fatalf("code = %q, want bad_payload", e.Code)
			}
		})
	}
}

// The retired SET_GLOBAL_HOTKEY number is an unknown frame now (logged
// and ignored, like any other), not a second way to set the chord.
func TestRetiredSetGlobalHotkeyFrameIgnored(t *testing.T) {
	d := newFrameTestDaemon(t)
	ops, _, _ := asyncOps("")
	d.handleControlFrame(context.Background(), ops, wire.FrameType(0x58), mustJSON(t, wire.GlobalHotkey{Enabled: false}))
	if got := d.reg.Dictation(); got != wire.DefaultDictation {
		t.Fatalf("frame 0x58 changed the settings to %+v", got)
	}
}

// A change reaches every subscriber with every setting, so a switch-only
// set still tells the menu bar the chord; setting the same values again
// is silent, so the GUI's reconcile cannot echo into a broadcast storm.
func TestSetDictationBroadcastsFullSettings(t *testing.T) {
	d := newFrameTestDaemon(t)
	ch, unsub := d.dictation.Subscribe()
	defer unsub()
	ops, _, _ := asyncOps("")
	on := true
	req := wire.SetDictationReq{HandyAutoStart: &on}
	d.handleControlFrame(context.Background(), ops, wire.FrameSetDictation, mustJSON(t, req))
	want := wire.Dictation{Hotkey: wire.DefaultGlobalHotkey, HandyAutoStart: true}
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("broadcast %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no DICTATION broadcast")
	}
	d.handleControlFrame(context.Background(), ops, wire.FrameSetDictation, mustJSON(t, req))
	select {
	case extra := <-ch:
		t.Fatalf("unchanged settings re-broadcast %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}
