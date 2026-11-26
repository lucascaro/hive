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

func hk(key uint32) *wire.GlobalHotkey {
	return &wire.GlobalHotkey{KeyCode: key, Modifiers: wire.HotkeyModCmd | wire.HotkeyModShift, Enabled: true}
}

func boolp(b bool) *bool { return &b }

func TestDictationDefaultWhenMissing(t *testing.T) {
	r := freshRegistry(t)
	if got := r.Dictation(); got != wire.DefaultDictation {
		t.Fatalf("Dictation = %+v, want default %+v", got, wire.DefaultDictation)
	}
}

func TestDictationPersists(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	req := wire.SetDictationReq{Hotkey: hk(2), HandyAutoStart: boolp(true)}
	changed, err := r.SetDictation(req)
	if err != nil || !changed {
		t.Fatalf("SetDictation = %v, %v; want true, nil", changed, err)
	}
	if changed, _ := r.SetDictation(req); changed {
		t.Error("setting the same values again reported a change")
	}
	_ = r.Close()
	r2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if got, want := r2.Dictation(), (wire.Dictation{Hotkey: *hk(2), HandyAutoStart: true}); got != want {
		t.Fatalf("after reopen = %+v, want %+v", got, want)
	}
}

// The GUI's keymap sync and its Settings toggle each write one field;
// neither may clobber the other.
func TestSetDictationMergesFields(t *testing.T) {
	r := freshRegistry(t)
	if _, err := r.SetDictation(wire.SetDictationReq{HandyAutoStart: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	if got := r.Dictation(); got.Hotkey != wire.DefaultGlobalHotkey || !got.HandyAutoStart {
		t.Fatalf("switch-only set = %+v", got)
	}
	if _, err := r.SetDictation(wire.SetDictationReq{Hotkey: hk(2)}); err != nil {
		t.Fatal(err)
	}
	if got := r.Dictation(); got.Hotkey != *hk(2) || !got.HandyAutoStart {
		t.Fatalf("hotkey-only set = %+v; the switch must survive", got)
	}
}

func TestDictationDisabledHotkeyClearsChord(t *testing.T) {
	r := freshRegistry(t)
	if _, err := r.SetDictation(wire.SetDictationReq{Hotkey: &wire.GlobalHotkey{KeyCode: 9, Modifiers: 0xffff}}); err != nil {
		t.Fatal(err)
	}
	if got := r.Dictation().Hotkey; got != (wire.GlobalHotkey{}) {
		t.Fatalf("disabled hotkey stored as %+v, want zero value", got)
	}
}

func TestSetDictationRejectsInvalid(t *testing.T) {
	r := freshRegistry(t)
	for _, req := range []wire.SetDictationReq{
		{},
		{Hotkey: &wire.GlobalHotkey{KeyCode: 128, Modifiers: wire.HotkeyModCmd, Enabled: true}},
		{Hotkey: &wire.GlobalHotkey{KeyCode: 49, Enabled: true}, HandyAutoStart: boolp(true)},
		{Hotkey: &wire.GlobalHotkey{KeyCode: 49, Modifiers: wire.HotkeyModCmd | 0x4000, Enabled: true}},
	} {
		if _, err := r.SetDictation(req); err == nil {
			t.Errorf("SetDictation(%+v) accepted", req)
		}
	}
	if got := r.Dictation(); got != wire.DefaultDictation {
		t.Fatalf("a rejected request changed the value to %+v", got)
	}
}

func writeState(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A user who rebound the chord under contract 36 keeps it.
func TestDictationMigratesLegacyHotkeyFile(t *testing.T) {
	dir := t.TempDir()
	writeState(t, dir, legacyHotkeyFile, `{"key_code":2,"modifiers":768,"enabled":true}`)
	if got, want := openAt(t, dir).Dictation(), (wire.Dictation{Hotkey: *hk(2)}); got != want {
		t.Fatalf("Dictation = %+v, want %+v", got, want)
	}
}

// Saving writes dictation.json and never the legacy file.
func TestDictationFirstSaveWritesNewFileOnly(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"key_code":2,"modifiers":768,"enabled":true}`
	writeState(t, dir, legacyHotkeyFile, legacy)
	r := openAt(t, dir)
	if _, err := r.SetDictation(wire.SetDictationReq{HandyAutoStart: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, legacyHotkeyFile)); string(b) != legacy {
		t.Fatalf("legacy file changed to %s", b)
	}
	var d wire.Dictation
	if err := readJSON(filepath.Join(dir, dictationFile), &d); err != nil || d != (wire.Dictation{Hotkey: *hk(2), HandyAutoStart: true}) {
		t.Fatalf("dictation.json = %+v, %v", d, err)
	}
}

// A corrupt or unusable file falls back to the default rather than
// failing Open, and once dictation.json exists the legacy file is not
// consulted again.
func TestDictationCorruptFileFallsBackToDefault(t *testing.T) {
	for name, c := range map[string]struct{ body, legacy string }{
		"garbage":             {"{not json", ""},
		"unusable":            {`{"hotkey":{"key_code":49,"modifiers":0,"enabled":true}}`, ""},
		"garbage with legacy": {"{not json", `{"key_code":2,"modifiers":768,"enabled":true}`},
		"legacy garbage":      {"", "{nope"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if c.body != "" {
				writeState(t, dir, dictationFile, c.body)
			}
			if c.legacy != "" {
				writeState(t, dir, legacyHotkeyFile, c.legacy)
			}
			if got := openAt(t, dir).Dictation(); got != wire.DefaultDictation {
				t.Fatalf("Dictation = %+v, want default", got)
			}
		})
	}
}

// Settings that cannot be written are not adopted, so the daemon never
// relays a value it would forget on restart.
func TestDictationWriteFailureKeepsValue(t *testing.T) {
	r := freshRegistry(t)
	// A regular file where the state dir's parent should be: no write or
	// mkdir under it can succeed.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.stateDir = filepath.Join(blocker, "state")
	if changed, err := r.SetDictation(wire.SetDictationReq{Hotkey: hk(2)}); err == nil || changed {
		t.Fatalf("SetDictation into an unwritable dir: changed %v, err %v", changed, err)
	}
	if got := r.Dictation(); got != wire.DefaultDictation {
		t.Fatalf("value after a failed write = %+v, want default", got)
	}
}
