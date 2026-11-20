// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"errors"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// pinPages splits a session's pin headers (newest first) into PINS
// frames of at most wire.PinsPageSize, Done on the last. There is no
// cap on how many pins a session holds, so one frame could outgrow
// wire.MaxPayload; no session ever answers zero frames.
func pinPages(sessionID string, pins []wire.Pin) []wire.PinsMsg {
	var out []wire.PinsMsg
	for len(pins) > wire.PinsPageSize {
		out = append(out, wire.PinsMsg{SessionID: sessionID, Pins: pins[:wire.PinsPageSize]})
		pins = pins[wire.PinsPageSize:]
	}
	if pins == nil {
		pins = []wire.Pin{}
	}
	return append(out, wire.PinsMsg{SessionID: sessionID, Pins: pins, Done: true})
}

// sendPinError maps a registry pin error to its wire code.
func sendPinError(ops controlOps, err error) {
	switch {
	case errors.Is(err, registry.ErrPinInvalid):
		ops.sendError(wire.ErrCodePinInvalid, err.Error())
	case errors.Is(err, registry.ErrPinTooLong):
		ops.sendError(wire.ErrCodePinTooLong, err.Error())
	case errors.Is(err, registry.ErrNoSuchPin):
		ops.sendError(wire.ErrCodeNoSuchPin, "that pin does not exist in this session")
	case errors.Is(err, registry.ErrNotFound):
		ops.sendError("no_such_session", "that session is not open")
	default:
		ops.sendError("pin_failed", err.Error())
	}
}
