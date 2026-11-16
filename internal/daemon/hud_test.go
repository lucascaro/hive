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

func TestSetGlobalHotkeyRefusesPlugin(t *testing.T) {
	d := newFrameTestDaemon(t)
	ops, errs, _ := asyncOps("plugin:acme")
	d.handleControlFrame(context.Background(), ops, wire.FrameSetGlobalHotkey, mustJSON(t, wire.GlobalHotkey{Enabled: false}))
	expectRefused(t, errs)
	if got := d.reg.GlobalHotkey(); got != wire.DefaultGlobalHotkey {
		t.Fatalf("a plugin changed the hotkey to %+v", got)
	}
}

func TestSetGlobalHotkeyRejectsInvalid(t *testing.T) {
	d := newFrameTestDaemon(t)
	ops, errs, _ := asyncOps("")
	d.handleControlFrame(context.Background(), ops, wire.FrameSetGlobalHotkey, mustJSON(t, wire.GlobalHotkey{KeyCode: 49, Enabled: true}))
	if e := <-errs; e.Code != "bad_payload" {
		t.Fatalf("code = %q, want bad_payload", e.Code)
	}
}

// A change reaches every subscriber; setting the same chord again is
// silent, so the GUI's reconcile cannot echo into a broadcast storm.
func TestSetGlobalHotkeyBroadcasts(t *testing.T) {
	d := newFrameTestDaemon(t)
	ch, unsub := d.hotkeys.Subscribe()
	defer unsub()
	ops, _, _ := asyncOps("")
	want := wire.GlobalHotkey{KeyCode: 2, Modifiers: wire.HotkeyModCmd | wire.HotkeyModOption, Enabled: true}
	d.handleControlFrame(context.Background(), ops, wire.FrameSetGlobalHotkey, mustJSON(t, want))
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("broadcast %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no GLOBAL_HOTKEY broadcast")
	}
	d.handleControlFrame(context.Background(), ops, wire.FrameSetGlobalHotkey, mustJSON(t, want))
	select {
	case extra := <-ch:
		t.Fatalf("unchanged chord re-broadcast %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}
