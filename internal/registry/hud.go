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

// globalHotkeyFile is the HUD chord's file under the state dir. It is a
// cache of the GUI's keymap.json entry, kept here so the menu bar, which
// reads nothing from the state dir, can learn it over the wire even
// when no GUI is running.
const globalHotkeyFile = "global-hotkey.json"

// loadGlobalHotkey reads the persisted chord. A missing file means the
// default. A file that will not parse, or holds a chord that cannot be
// registered, is logged and replaced by the default: the GUI rewrites
// the cache from keymap.json at its next boot, so nothing the user typed
// is lost by ignoring it.
func (r *Registry) loadGlobalHotkey() wire.GlobalHotkey {
	path := filepath.Join(r.stateDir, globalHotkeyFile)
	var h wire.GlobalHotkey
	if err := readJSON(path, &h); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("registry: %s: %v; using the default hotkey", path, err)
		}
		return wire.DefaultGlobalHotkey
	}
	if !h.Valid() {
		log.Printf("registry: %s holds an unusable hotkey %+v; using the default", path, h)
		return wire.DefaultGlobalHotkey
	}
	return h
}

// GlobalHotkey returns the chord that opens the capture HUD.
func (r *Registry) GlobalHotkey() wire.GlobalHotkey {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.globalHotkey
}

// SetGlobalHotkey validates, persists and adopts h. It reports whether
// the value changed, so the caller broadcasts only a real change.
func (r *Registry) SetGlobalHotkey(h wire.GlobalHotkey) (bool, error) {
	if !h.Valid() {
		return false, fmt.Errorf("invalid hotkey %+v", h)
	}
	if !h.Enabled {
		h.KeyCode, h.Modifiers = 0, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if h == r.globalHotkey {
		return false, nil
	}
	if err := writeJSON(filepath.Join(r.stateDir, globalHotkeyFile), h); err != nil {
		return false, err
	}
	r.globalHotkey = h
	return true, nil
}
