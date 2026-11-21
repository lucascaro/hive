// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// State behind the menu-bar capture HUD (spec 553): which session a GUI
// last focused, and the system-wide chord that opens the HUD. Neither
// carries text for a session; the HUD delivers through SendToSession.

package registry

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// focusRepeatWindow is how long a repeat report for the session that is
// already the newest focus is ignored. A GUI re-asserts focus on window
// activation as well as on a switch, and several windows can report the
// same click; each report would otherwise broadcast a SESSION_EVENT to
// every client.
const focusRepeatWindow = time.Second

// nowMillis is the focus clock. A var so tests can pin it.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// SetFocused records that a GUI is showing session id to the user,
// stamping Entry.focusedAt and announcing it as SessionEventFocus. The
// stamp is in-memory only: it never reaches session.json.
//
// Stamps are strictly increasing across sessions, so "newest focus" is
// a total order even when two reports land in the same millisecond.
func (r *Registry) SetFocused(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return ErrNotFound
	}
	now := nowMillis()
	if r.lastFocusID == id && now-r.lastFocusAt < focusRepeatWindow.Milliseconds() {
		return nil
	}
	if now <= r.lastFocusAt {
		now = r.lastFocusAt + 1
	}
	e.focusedAt = now
	r.lastFocusID, r.lastFocusAt = id, now
	r.broadcastLocked(wire.SessionEventFocus, e.Info())
	return nil
}

// dictationFile holds the capture HUD's settings (spec 573). The chord
// is a cache of the GUI's keymap.json entry and the Handy switch is the
// GUI's Settings toggle; both are kept here so the menu bar, which
// reads nothing from the state dir, can learn them over the wire even
// when no GUI is running.
const dictationFile = "dictation.json"

// legacyHotkeyFile is where contract 36 kept the chord alone. It is read
// once, when dictationFile does not exist yet, so a rebound chord
// survives the upgrade. It is never written or removed.
const legacyHotkeyFile = "global-hotkey.json"

// loadDictation reads the persisted settings. With no dictation.json it
// takes the chord from the legacy file, if that holds a usable one. A
// dictation.json that will not parse, or holds a chord that cannot be
// registered, is logged and replaced by the default rather than by the
// legacy chord: once written it is the truth, and the GUI rewrites the
// chord from keymap.json at its next boot anyway.
func (r *Registry) loadDictation() wire.Dictation {
	path := filepath.Join(r.stateDir, dictationFile)
	var d wire.Dictation
	err := readJSON(path, &d)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return r.loadLegacyHotkey()
	case err != nil:
		log.Printf("registry: %s: %v; using the default dictation settings", path, err)
		return wire.DefaultDictation
	case !d.Hotkey.Valid():
		log.Printf("registry: %s holds an unusable hotkey %+v; using the default", path, d.Hotkey)
		d.Hotkey = wire.DefaultGlobalHotkey
	}
	return d
}

func (r *Registry) loadLegacyHotkey() wire.Dictation {
	d := wire.DefaultDictation
	path := filepath.Join(r.stateDir, legacyHotkeyFile)
	var h wire.GlobalHotkey
	if err := readJSON(path, &h); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("registry: %s: %v; using the default hotkey", path, err)
		}
		return d
	}
	if h.Valid() {
		d.Hotkey = h
	}
	return d
}

// Dictation returns the capture HUD's settings.
func (r *Registry) Dictation() wire.Dictation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dictation
}

// SetDictation validates req, merges the settings it names, persists
// and adopts the result. It reports whether anything changed, so the
// caller broadcasts only a real change.
func (r *Registry) SetDictation(req wire.SetDictationReq) (bool, error) {
	if !req.Valid() {
		return false, fmt.Errorf("invalid dictation settings")
	}
	if h := req.Hotkey; h != nil && !h.Enabled {
		off := wire.GlobalHotkey{}
		req.Hotkey = &off
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := req.Apply(r.dictation)
	if next == r.dictation {
		return false, nil
	}
	if err := writeJSON(filepath.Join(r.stateDir, dictationFile), next); err != nil {
		return false, err
	}
	r.dictation = next
	return true, nil
}
