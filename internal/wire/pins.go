// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package wire

// Pinned responses (spec 566). An agent pins a response it wants the
// user to be able to recall — a title and a markdown body it writes
// itself — to its own session. The daemon keeps a session's pins on
// disk with the session, so they outlive a restart, a revive and a
// daemon restart, and are dropped only when the session's record is.
//
// There is no cap on how many pins a session holds, so the whole set
// can outgrow one frame (MaxPayload). The list therefore travels as
// headers only, in pages (PINS, Done on the last), and a body is
// fetched on its own (GET_PIN_BODY) when the user opens that pin.

const (
	// MaxPinTitleLen and MaxPinBodyLen bound one pin, in bytes, at the
	// daemon's trust boundary. Rejected, never truncated: a pin the
	// agent is told to shorten beats one silently cut mid-sentence.
	// These are safety bounds, not a product limit on what users pin.
	MaxPinTitleLen = 256
	MaxPinBodyLen  = 64 << 10
	// PinsPageSize is the most headers one PINS frame carries. 200
	// maximal headers are ~70 KiB, far under MaxPayload.
	PinsPageSize = 200

	// ErrCodePinInvalid: ADD_PIN with an empty title or body, or text
	// that is not valid UTF-8.
	ErrCodePinInvalid = "pin_invalid"
	// ErrCodePinTooLong: ADD_PIN whose title or body exceeds its bound.
	ErrCodePinTooLong = "pin_too_long"
	// ErrCodeNoSuchPin: the pin does not exist in that session — never
	// "exists, but in another session".
	ErrCodeNoSuchPin = "no_such_pin"
)

// Pin is one pin's header: everything but the body. CreatedAt is unix
// milliseconds on the daemon's clock. IDs sort in creation order.
type Pin struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
	BodyLen   int    `json:"body_len"`
}

// AddPinReq is ADD_PIN, sent only on a session connection: the session
// is the one the HELLO named, so the payload deliberately has no
// session field to forge.
type AddPinReq struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// AddPinOK answers ADD_PIN.
type AddPinOK struct {
	ID string `json:"id"`
}

// GetPinsReq asks for a session's pin headers. The answer is one or
// more PINS frames.
type GetPinsReq struct {
	SessionID string `json:"session_id"`
}

// PinsMsg is one page of a session's pin headers, newest first. Done
// marks the last page; a session with no pins answers one empty page
// with Done set.
type PinsMsg struct {
	SessionID string `json:"session_id"`
	Pins      []Pin  `json:"pins"`
	Done      bool   `json:"done"`
}

// GetPinBodyReq asks for one pin's body.
type GetPinBodyReq struct {
	SessionID string `json:"session_id"`
	ID        string `json:"id"`
}

// PinBodyMsg answers GET_PIN_BODY.
type PinBodyMsg struct {
	SessionID string `json:"session_id"`
	ID        string `json:"id"`
	Body      string `json:"body"`
}

// UnpinReq removes one pin. ID is required: "all" is UNPIN_ALL, never
// an empty id.
type UnpinReq struct {
	SessionID string `json:"session_id"`
	ID        string `json:"id"`
}

// UnpinAllReq removes every pin in a session.
type UnpinAllReq struct {
	SessionID string `json:"session_id"`
}

// PinsChangedMsg is the delta every control client gets when a
// session's pins change: one pin added, some removed, or all cleared.
type PinsChangedMsg struct {
	SessionID  string   `json:"session_id"`
	Added      *Pin     `json:"added,omitempty"`
	RemovedIDs []string `json:"removed_ids,omitempty"`
	Cleared    bool     `json:"cleared,omitempty"`
}
