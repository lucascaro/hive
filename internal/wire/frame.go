// Package wire is the hived ↔ hive client protocol (v0).
//
// Frame layout: 1-byte type, 4-byte big-endian length, payload.
//
//	+-------+-------------+--------------+
//	| type  | len (BE u32)| payload      |
//	| 1 B   | 4 B         | len B        |
//	+-------+-------------+--------------+
//
// DATA frames carry raw PTY bytes; control frames carry JSON. The type
// byte selects the decoder. See PROTOCOL_VERSION below for the version
// this package implements.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// PROTOCOL_VERSION is the version of the wire protocol this package
// implements. Bumped only when a breaking change is made; new frame
// types (added monotonically) and new JSON fields (ignored if unknown)
// do not require a version bump.
const PROTOCOL_VERSION = 0

// MaxPayload caps a single frame at 1 MiB. Anything larger is treated
// as a fatal protocol error.
const MaxPayload = 1 << 20

// FrameType is the 1-byte frame discriminator.
type FrameType byte

const (
	FrameHello   FrameType = 0x01 // C → S, JSON
	FrameWelcome FrameType = 0x02 // S → C, JSON
	FrameData    FrameType = 0x03 // both, raw bytes
	FrameResize  FrameType = 0x04 // C → S, JSON
	FrameEvent   FrameType = 0x05 // S → C, JSON
	FrameError   FrameType = 0x06 // S → C, JSON
)

func (t FrameType) String() string {
	switch t {
	case FrameHello:
		return "HELLO"
	case FrameWelcome:
		return "WELCOME"
	case FrameData:
		return "DATA"
	case FrameResize:
		return "RESIZE"
	case FrameEvent:
		return "EVENT"
	case FrameError:
		return "ERROR"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", byte(t))
	}
}

// ErrFrameTooLarge is returned when a frame's declared length exceeds
// MaxPayload. The connection should be dropped.
var ErrFrameTooLarge = errors.New("wire: frame exceeds max payload")

// WriteFrame writes a single framed message to w.
func WriteFrame(w io.Writer, t FrameType, payload []byte) error {
	if len(payload) > MaxPayload {
		return ErrFrameTooLarge
	}
	var hdr [5]byte
	hdr[0] = byte(t)
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads a single framed message from r. Returns ErrFrameTooLarge
// if the declared length is over the cap.
func ReadFrame(r io.Reader) (FrameType, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > MaxPayload {
		return 0, nil, ErrFrameTooLarge
	}
	if n == 0 {
		return FrameType(hdr[0]), nil, nil
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return FrameType(hdr[0]), payload, nil
}
