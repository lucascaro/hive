// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build e2e

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// TestE2E_DictationSentAfterHelloAndBroadcast: every control client
// learns the capture HUD's settings right after WELCOME (the menu bar
// has no other way to read them), a change made by one client reaches
// the others with every setting, and both survive a daemon restart.
func TestE2E_DictationSentAfterHelloAndBroadcast(t *testing.T) {
	skipIfWindows(t)
	d := spawnDaemon(t)
	watcher := dialControl(t, d)
	if got, err := watcher.AwaitDictation(3 * time.Second); err != nil || got != wire.DefaultDictation {
		t.Fatalf("initial DICTATION = %+v, %v; want default", got, err)
	}

	setter := dialControl(t, d)
	h := wire.GlobalHotkey{KeyCode: 2, Modifiers: wire.HotkeyModCmd | wire.HotkeyModShift, Enabled: true}
	if err := setter.SetDictation(wire.SetDictationReq{Hotkey: &h}); err != nil {
		t.Fatal(err)
	}
	if got, err := watcher.AwaitDictation(3 * time.Second); err != nil || got != (wire.Dictation{Hotkey: h}) {
		t.Fatalf("hotkey broadcast = %+v, %v", got, err)
	}
	on := true
	if err := setter.SetDictation(wire.SetDictationReq{HandyAutoStart: &on}); err != nil {
		t.Fatal(err)
	}
	want := wire.Dictation{Hotkey: h, HandyAutoStart: true}
	if got, err := watcher.AwaitDictation(3 * time.Second); err != nil || got != want {
		t.Fatalf("switch broadcast = %+v, %v; want %+v", got, err, want)
	}

	d.stop(t)
	d.cmd = nil // suppress the duplicate stop in t.Cleanup
	_ = os.Remove(d.sockPath)
	d2 := startBinary(t, d.sockPath, d.stateDir, filepath.Dir(d.sockPath))
	waitForSocket(t, d.sockPath, 5*time.Second)
	c := dialControl(t, d2)
	if got, err := c.AwaitDictation(3 * time.Second); err != nil || got != want {
		t.Fatalf("after restart = %+v, %v; want %+v", got, err, want)
	}
}

// TestE2E_FocusReportStampsSession: SET_FOCUSED_SESSION moves the
// session's focused_at, and other clients see it as a "focus" event.
func TestE2E_FocusReportStampsSession(t *testing.T) {
	skipIfWindows(t)
	d := spawnDaemon(t)
	id := firstSession(t, d)
	watcher := dialControl(t, d)
	gui := dialControl(t, d)
	if err := gui.SetFocusedSession(id); err != nil {
		t.Fatal(err)
	}
	ev, err := watcher.AwaitSessionEvent(wire.SessionEventFocus, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Session.ID != id || ev.Session.FocusedAt <= 0 {
		t.Fatalf("focus event = %s at %d, want %s stamped", ev.Session.ID, ev.Session.FocusedAt, id)
	}
}
