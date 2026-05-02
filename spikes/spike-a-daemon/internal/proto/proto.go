// Package proto is the throwaway wire protocol for Spike A.
//
// Frame layout: 1-byte type, 4-byte big-endian length, payload.
// This is intentionally minimal — Phase 1 will design the real protocol
// using the lessons learned here.
package proto

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// FrameData carries raw bytes in either direction.
	// Client→server: keystrokes destined for the PTY master.
	// Server→client: PTY output (including replay-buffer contents on attach).
	FrameData byte = 0x01

	// FrameResize is client→server only. Payload is 4 bytes:
	// big-endian uint16 cols, then big-endian uint16 rows.
	FrameResize byte = 0x02
)

const maxPayload = 1 << 20 // 1 MiB sanity cap; spike, not production.

// WriteFrame writes a single framed message.
func WriteFrame(w io.Writer, t byte, payload []byte) error {
	if len(payload) > maxPayload {
		return fmt.Errorf("payload too large: %d", len(payload))
	}
	var hdr [5]byte
	hdr[0] = t
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

// ReadFrame reads a single framed message.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > maxPayload {
		return 0, nil, fmt.Errorf("frame too large: %d", n)
	}
	if n == 0 {
		return hdr[0], nil, nil
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}

// EncodeResize packs cols/rows into a 4-byte payload.
func EncodeResize(cols, rows uint16) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint16(out[0:2], cols)
	binary.BigEndian.PutUint16(out[2:4], rows)
	return out
}

// DecodeResize unpacks a 4-byte payload into cols/rows.
func DecodeResize(payload []byte) (cols, rows uint16, ok bool) {
	if len(payload) < 4 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(payload[0:2]), binary.BigEndian.Uint16(payload[2:4]), true
}

// SocketPath returns the spike's Unix socket path. On Unix it lives in
// /tmp tagged with the caller's UID for isolation; on Windows it lives
// under os.TempDir() tagged with the username (Windows 10+ supports
// AF_UNIX). The uid argument is ignored on Windows.
func SocketPath(uid int) string {
	if uid >= 0 {
		return fmt.Sprintf("/tmp/hived-spike-%d.sock", uid)
	}
	user := os.Getenv("USERNAME")
	if user == "" {
		user = "default"
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("hived-spike-%s.sock", user))
}
