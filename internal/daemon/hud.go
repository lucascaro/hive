// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"errors"
	"log"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// handleHUDFrame serves the two capture-HUD requests (specs 553, 573).
//
// Both are refused to plugins. SET_FOCUSED_SESSION picks the session the
// HUD offers by default, so a plugin able to send it could steer what
// the user's dictated text lands in; SET_DICTATION rebinds or disables a
// system-wide key and decides whether opening the HUD turns a
// microphone on. They sit with the other verbs a plugin
// socket is refused: SEND_TO_SESSION, input on a plugin's attach
// (serveAttach) and pluginRefusedFrames (daemon.go). docs/plugins.md
// lists them all. The refusal is per socket, not a sandbox: a plugin
// runs as the user.
func (d *Daemon) handleHUDFrame(ops controlOps, ft wire.FrameType, payload []byte) {
	if ops.principal != "" {
		ops.sendError(wire.ErrCodeModeNotAllowed, ft.String()+" is not available to plugins")
		return
	}
	switch ft {
	case wire.FrameSetFocusedSession:
		req, ok := decodeReq[wire.SetFocusedSessionReq](payload, ops.sendError)
		if !ok {
			return
		}
		if err := d.reg.SetFocused(req.SessionID); err != nil {
			code := "focus_failed"
			if errors.Is(err, registry.ErrNotFound) {
				code = "no_such_session"
			}
			_ = ops.writeJSON(wire.FrameError, wire.Error{Code: code, Message: err.Error(), SessionID: req.SessionID})
		}
	case wire.FrameSetDictation:
		req, ok := decodeReq[wire.SetDictationReq](payload, ops.sendError)
		if !ok {
			return
		}
		if !req.Valid() {
			ops.sendError("bad_payload", "SET_DICTATION: name at least one setting; a hotkey's key_code must be < 128 with at least one of cmd/shift/option/control and no other modifier bits")
			return
		}
		changed, err := d.reg.SetDictation(req)
		if err != nil {
			ops.sendError("dictation_failed", err.Error())
			return
		}
		if changed {
			got := d.reg.Dictation()
			log.Printf("daemon: dictation set key_code=%d modifiers=%#x enabled=%v handy_auto_start=%v",
				got.Hotkey.KeyCode, got.Hotkey.Modifiers, got.Hotkey.Enabled, got.HandyAutoStart)
			d.dictation.Publish(got)
		}
	}
}
