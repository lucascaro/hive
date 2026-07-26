package wire

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client is one handshaken connection to hived in a single wire mode.
// It is the shared protocol client used by the GUI, the ws-bridge and
// the e2e testclient — the dial policy (spawn-on-miss, timeouts) stays
// with the caller, everything after the socket exists lives here.
//
// Writes are serialized: WriteFrame issues two Writes (header, then
// payload), so unserialized concurrent writers would interleave
// mid-frame and corrupt the stream. Reads are lock-free — each Client
// has exactly one reader goroutine by contract.
type Client struct {
	conn    net.Conn
	wmu     sync.Mutex
	welcome Welcome
}

// handshakeTimeout bounds the wait for WELCOME. A daemon that accepts
// the socket but never answers must fail the caller, not hang it —
// the GUI's ConnectControl/OpenSession have no other watchdog. A var,
// not a const, so tests can shrink it.
var handshakeTimeout = 5 * time.Second

// Handshake sends HELLO on an established conn, waits for WELCOME and
// returns the wrapped Client. The conn is closed on any failure.
// hello.Version is filled with PROTOCOL_VERSION when zero (left alone
// otherwise so tests can probe version rejection). The WELCOME wait is
// bounded by handshakeTimeout; any caller-set read deadline is
// overwritten and cleared on success.
func Handshake(conn net.Conn, hello Hello) (*Client, error) {
	if hello.Version == 0 {
		hello.Version = PROTOCOL_VERSION
	}
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	if err := WriteJSON(conn, FrameHello, hello); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	ft, payload, err := ReadFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("welcome: %w", err)
	}
	switch ft {
	case FrameWelcome:
		var w Welcome
		if err := json.Unmarshal(payload, &w); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("welcome: %w", err)
		}
		_ = conn.SetReadDeadline(time.Time{})
		return &Client{conn: conn, welcome: w}, nil
	case FrameError:
		_ = conn.Close()
		var werr Error
		if json.Unmarshal(payload, &werr) == nil && werr.Message != "" {
			return nil, fmt.Errorf("daemon refused connection: %s", werr.Message)
		}
		return nil, fmt.Errorf("daemon refused connection")
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("expected WELCOME, got %s", ft)
	}
}

// NewClient wraps an already-handshaken (or test) conn without
// performing the HELLO/WELCOME exchange. Welcome() is zero.
func NewClient(conn net.Conn) *Client { return &Client{conn: conn} }

// Welcome returns the WELCOME frame received during Handshake.
func (c *Client) Welcome() Welcome { return c.welcome }

// WriteJSON marshals v and writes it as a frame of type t, serialized
// against other writers on this Client.
func (c *Client) WriteJSON(t FrameType, v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteJSON(c.conn, t, v)
}

// WriteFrame writes a raw frame, serialized against other writers.
func (c *Client) WriteFrame(t FrameType, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteFrame(c.conn, t, payload)
}

// ReadFrame reads the next frame. Single-reader contract: only one
// goroutine may call ReadFrame.
func (c *Client) ReadFrame() (FrameType, []byte, error) {
	return ReadFrame(c.conn)
}

// Close is deliberately not guarded by the write mutex: net.Conn.Close
// is safe concurrently with a blocked Write and must be able to
// unblock one.
func (c *Client) Close() error { return c.conn.Close() }

// controlEvents and attachEvents are the shared frame → UI-event
// dispatch tables. The GUI (Wails EventsEmit) and the ws-bridge (WS
// notifications) emit identical event names; a new fanout frame is
// added here once and every client picks it up.
var controlEvents = map[FrameType]string{
	FrameSessions:     "session:list",
	FrameSessionEvent: "session:event",
	FrameProjects:     "project:list",
	FrameProjectEvent: "project:event",
	FrameError:        "control:error",
}

var attachEvents = map[FrameType]string{
	FrameData:  "pty:data",
	FrameEvent: "pty:event",
	FrameError: "pty:error",
}

// ControlEventName maps a control-mode frame to its client event name.
// ok is false for frames that are not part of the control fanout.
func ControlEventName(t FrameType) (string, bool) {
	n, ok := controlEvents[t]
	return n, ok
}

// AttachEventName maps an attach-mode frame to its client event name.
// ok is false for frames not part of the attach fanout. "pty:data"
// payloads are raw PTY bytes (callers base64 them for JS transport);
// the others are JSON.
func AttachEventName(t FrameType) (string, bool) {
	n, ok := attachEvents[t]
	return n, ok
}
