// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// pinClock makes nowMillis return *now for the test's duration.
func pinClock(t *testing.T, now *int64) {
	t.Helper()
	prev := nowMillis
	nowMillis = func() int64 { return *now }
	t.Cleanup(func() { nowMillis = prev })
}

func TestSetFocusedStampsAndEmits(t *testing.T) {
	skipOnWindows(t)
	now := int64(1_000_000)
	pinClock(t, &now)
	r := freshRegistry(t)
	a := mustCreate(t, r, wire.CreateSpec{Name: "a"})
	b := mustCreate(t, r, wire.CreateSpec{Name: "b"})
	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)

	if err := r.SetFocused(a.ID); err != nil {
		t.Fatalf("SetFocused: %v", err)
	}
	ev := nextEvent(t, ch)
	if ev.Kind != wire.SessionEventFocus || ev.Session.ID != a.ID || ev.Session.FocusedAt != now {
		t.Fatalf("event = %s/%s/%d, want focus/%s/%d", ev.Kind, ev.Session.ID, ev.Session.FocusedAt, a.ID, now)
	}
	// Same millisecond, other session: the stamp still moves forward,
	// so "newest focus" stays a total order.
	if err := r.SetFocused(b.ID); err != nil {
		t.Fatalf("SetFocused: %v", err)
	}
	ev = nextEvent(t, ch)
	if ev.Session.ID != b.ID || ev.Session.FocusedAt <= now {
		t.Fatalf("second stamp = %d for %s, want > %d for %s", ev.Session.FocusedAt, ev.Session.ID, now, b.ID)
	}
	for _, info := range r.List() {
		if info.ID == a.ID && info.FocusedAt != now {
			t.Errorf("List lost a's stamp: %d", info.FocusedAt)
		}
	}
}

func TestSetFocusedRepeatNoEvent(t *testing.T) {
	skipOnWindows(t)
	now := int64(5_000_000)
	pinClock(t, &now)
	r := freshRegistry(t)
	a := mustCreate(t, r, wire.CreateSpec{Name: "a"})
	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)
	if err := r.SetFocused(a.ID); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, ch)
	now += 500
	if err := r.SetFocused(a.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case extra := <-ch:
		t.Fatalf("repeat report within the window broadcast %+v", extra)
	default:
	}
	// Past the window the same session is announced again: a GUI
	// coming back to the front is a real "the user is looking".
	now += focusRepeatWindow.Milliseconds()
	if err := r.SetFocused(a.ID); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, ch); ev.Kind != wire.SessionEventFocus {
		t.Fatalf("kind = %q after the window", ev.Kind)
	}
}

// The storm guard keys off in-memory state, so a fresh registry (a
// daemon restart) always takes its first report.
func TestSetFocusedFirstReportAfterRestartEmits(t *testing.T) {
	skipOnWindows(t)
	now := int64(9_000_000)
	pinClock(t, &now)
	r := freshRegistry(t)
	a := mustCreate(t, r, wire.CreateSpec{Name: "a"})
	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)
	if err := r.SetFocused(a.ID); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, ch); ev.Kind != wire.SessionEventFocus {
		t.Fatalf("kind = %q", ev.Kind)
	}
}

func TestSetFocusedUnknownSession(t *testing.T) {
	r := freshRegistry(t)
	if err := r.SetFocused("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// focused_at is in-memory: it must never reach session.json.
func TestSetFocusedNotPersisted(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	a := mustCreate(t, r, wire.CreateSpec{Name: "a"})
	if err := r.SetFocused(a.ID); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(SessionsDir(r.stateDir), a.ID, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "focus") {
		t.Fatalf("session.json mentions focus:\n%s", b)
	}
}

func TestGlobalHotkeyDefaultWhenMissing(t *testing.T) {
	r := freshRegistry(t)
	if got := r.GlobalHotkey(); got != wire.DefaultGlobalHotkey {
		t.Fatalf("GlobalHotkey = %+v, want default %+v", got, wire.DefaultGlobalHotkey)
	}
}

func TestGlobalHotkeyPersists(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := wire.GlobalHotkey{KeyCode: 2, Modifiers: wire.HotkeyModCmd | wire.HotkeyModShift, Enabled: true}
	changed, err := r.SetGlobalHotkey(want)
	if err != nil || !changed {
		t.Fatalf("SetGlobalHotkey = %v, %v; want true, nil", changed, err)
	}
	if changed, _ := r.SetGlobalHotkey(want); changed {
		t.Error("setting the same chord again reported a change")
	}
	_ = r.Close()
	r2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if got := r2.GlobalHotkey(); got != want {
		t.Fatalf("after reopen = %+v, want %+v", got, want)
	}
}

func TestGlobalHotkeyDisabledClearsChord(t *testing.T) {
	r := freshRegistry(t)
	if _, err := r.SetGlobalHotkey(wire.GlobalHotkey{KeyCode: 9, Modifiers: 0xffff, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if got := r.GlobalHotkey(); got != (wire.GlobalHotkey{}) {
		t.Fatalf("disabled hotkey stored as %+v, want zero value", got)
	}
}

func TestGlobalHotkeyRejectsInvalid(t *testing.T) {
	r := freshRegistry(t)
	for _, h := range []wire.GlobalHotkey{
		{KeyCode: 128, Modifiers: wire.HotkeyModCmd, Enabled: true},
		{KeyCode: 49, Modifiers: 0, Enabled: true},
		{KeyCode: 49, Modifiers: wire.HotkeyModCmd | 0x4000, Enabled: true},
	} {
		if _, err := r.SetGlobalHotkey(h); err == nil {
			t.Errorf("SetGlobalHotkey(%+v) accepted", h)
		}
	}
	if got := r.GlobalHotkey(); got != wire.DefaultGlobalHotkey {
		t.Fatalf("a rejected chord changed the value to %+v", got)
	}
}

// A corrupt or unusable file falls back to the default rather than
// failing Open: the file is a cache the GUI rewrites from keymap.json.
func TestGlobalHotkeyCorruptFileFallsBackToDefault(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":  "{not json",
		"unusable": `{"key_code":49,"modifiers":0,"enabled":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, globalHotkeyFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			r, err := Open(dir)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			if got := r.GlobalHotkey(); got != wire.DefaultGlobalHotkey {
				t.Fatalf("GlobalHotkey = %+v, want default", got)
			}
		})
	}
}
