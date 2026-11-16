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

// handleHUDFrame serves the two capture-HUD requests (spec 553).
//
// Both are refused to plugins. SET_FOCUSED_SESSION picks the session the
// HUD offers by default, so a plugin able to send it could steer what
// the user's dictated text lands in; SET_GLOBAL_HOTKEY rebinds or
// disables a system-wide key. Neither is something a plugin has been
// granted, the same line SEND_TO_SESSION draws.
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
	case wire.FrameSetGlobalHotkey:
		req, ok := decodeReq[wire.GlobalHotkey](payload, ops.sendError)
		if !ok {
			return
		}
		if !req.Valid() {
			ops.sendError("bad_payload", "SET_GLOBAL_HOTKEY: key_code must be < 128 with at least one of cmd/shift/option/control and no other modifier bits")
			return
		}
		changed, err := d.reg.SetGlobalHotkey(req)
		if err != nil {
			ops.sendError("hotkey_failed", err.Error())
			return
		}
		if changed {
			log.Printf("daemon: global hotkey set key_code=%d modifiers=%#x enabled=%v", req.KeyCode, req.Modifiers, req.Enabled)
			d.hotkeys.Publish(d.reg.GlobalHotkey())
		}
	}
}
