// Package testclient is a small async client over the binary hived
// wire protocol, intended for end-to-end tests that spawn the real
// hived binary as a subprocess.
//
// It is NOT a production client — it skips the niceties (build_id
// negotiation, reconnect, flow control) and is designed to make tests
// read like the GUI does: "open control conn, create session, attach,
// wait for marker in scrollback".
//
// Isolation contract: callers MUST set HIVE_SOCKET and HIVE_STATE_DIR
// to temp paths before dialing. RequireIsolation asserts both are
// present and point under a recognised temp prefix; production state
// is never touched.
package testclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// Client is a single connection to hived in one of the wire modes
// (control / attach / create). The reader goroutine demultiplexes
// frames into channels grouped by who consumes them:
//
//   - stream:   DATA + EVENT in one ordered channel. Splitting these
//     across two channels loses select-ordering, which matters for
//     the Begin → DATA → Done replay envelope.
//   - sessions / projects: control-mode broadcast channels.
//   - snaps:    one-shot frames (WELCOME, ERROR, SESSIONS, PROJECTS).
type Client struct {
	conn net.Conn

	stream   chan frameMsg
	sessions chan wire.SessionEvent
	projects chan wire.ProjectEvent
	snaps    chan frameMsg
	errs     chan error
	welcome  wire.Welcome

	closeOnce sync.Once
	closed    chan struct{}
}

type frameMsg struct {
	t       wire.FrameType
	payload []byte
}

// Dial opens a connection to the daemon's unix socket and starts the
// reader goroutine. Handshake is a separate call so the caller can
// fail fast on missing isolation env vars before opening sockets.
func Dial(ctx context.Context, sockPath string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("testclient: dial %s: %w", sockPath, err)
	}
	c := &Client{
		conn:     conn,
		stream:   make(chan frameMsg, 256),
		sessions: make(chan wire.SessionEvent, 32),
		projects: make(chan wire.ProjectEvent, 32),
		snaps:    make(chan frameMsg, 8),
		errs:     make(chan error, 1),
		closed:   make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Handshake sends HELLO and reads the WELCOME (or ERROR) reply.
// The client version is filled in for the caller.
func (c *Client) Handshake(hello wire.Hello) (wire.Welcome, error) {
	if hello.Version == 0 {
		hello.Version = wire.PROTOCOL_VERSION
	}
	if hello.Client == "" {
		hello.Client = "testclient/0"
	}
	if err := wire.WriteJSON(c.conn, wire.FrameHello, hello); err != nil {
		return wire.Welcome{}, fmt.Errorf("testclient: write HELLO: %w", err)
	}
	// The reader goroutine is already running. WELCOME / ERROR arrive
	// via the snapshot channel (anything that isn't DATA/EVENT/
	// SESSION_EVENT/PROJECT_EVENT lands there).
	select {
	case msg := <-c.snaps:
		switch msg.t {
		case wire.FrameWelcome:
			var w wire.Welcome
			if err := json.Unmarshal(msg.payload, &w); err != nil {
				return wire.Welcome{}, fmt.Errorf("testclient: decode WELCOME: %w", err)
			}
			c.welcome = w
			return w, nil
		case wire.FrameError:
			var e wire.Error
			_ = json.Unmarshal(msg.payload, &e)
			return wire.Welcome{}, fmt.Errorf("testclient: daemon refused handshake: %s: %s", e.Code, e.Message)
		default:
			return wire.Welcome{}, fmt.Errorf("testclient: unexpected frame during handshake: %s", msg.t)
		}
	case err := <-c.errs:
		return wire.Welcome{}, fmt.Errorf("testclient: handshake read: %w", err)
	case <-time.After(5 * time.Second):
		return wire.Welcome{}, errors.New("testclient: handshake timeout")
	}
}

// Welcome returns the daemon's WELCOME response captured during Handshake.
func (c *Client) Welcome() wire.Welcome { return c.welcome }

// WriteStdin sends raw PTY bytes (an attach-mode DATA frame).
func (c *Client) WriteStdin(b []byte) error {
	return wire.WriteFrame(c.conn, wire.FrameData, b)
}

// Resize sends a RESIZE frame.
func (c *Client) Resize(cols, rows int) error {
	return wire.WriteJSON(c.conn, wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
}

// RequestReplay asks the daemon to re-stream the scrollback ring buffer.
func (c *Client) RequestReplay() error {
	return wire.WriteFrame(c.conn, wire.FrameRequestReplay, nil)
}

// CreateSession sends a CREATE_SESSION control frame. Caller must be
// in control mode.
func (c *Client) CreateSession(spec wire.CreateSpec) error {
	return wire.WriteJSON(c.conn, wire.FrameCreateSession, spec)
}

// KillSession sends KILL_SESSION.
func (c *Client) KillSession(req wire.KillSessionReq) error {
	return wire.WriteJSON(c.conn, wire.FrameKillSession, req)
}

// ListSessions sends LIST_SESSIONS. Use AwaitSessionsSnapshot to
// consume the response.
func (c *Client) ListSessions() error {
	return wire.WriteJSON(c.conn, wire.FrameListSessions, wire.ListSessionsReq{})
}

// WaitForData reads from the stream until the accumulated DATA bytes
// contain needle, or timeout. Returns the full accumulator on hit so
// callers can assert on more than just the needle.
//
// Replay envelope events (Begin/Done) are silently absorbed so callers
// don't need to interleave reads. session_exit and other unexpected
// events are surfaced as errors to prevent hangs.
func (c *Client) WaitForData(needle []byte, timeout time.Duration) ([]byte, error) {
	var buf bytes.Buffer
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return buf.Bytes(), fmt.Errorf("testclient: timeout waiting for %q (got %d bytes)", needle, buf.Len())
		}
		select {
		case msg := <-c.stream:
			switch msg.t {
			case wire.FrameData:
				buf.Write(msg.payload)
				if bytes.Contains(buf.Bytes(), needle) {
					return buf.Bytes(), nil
				}
			case wire.FrameEvent:
				var ev wire.Event
				_ = json.Unmarshal(msg.payload, &ev)
				if ev.Kind == wire.EventScrollbackReplayBegin || ev.Kind == wire.EventScrollbackReplayDone {
					continue
				}
				return buf.Bytes(), fmt.Errorf("testclient: unexpected event %q while waiting for %q", ev.Kind, needle)
			}
		case err := <-c.errs:
			return buf.Bytes(), err
		case <-time.After(remaining):
			return buf.Bytes(), fmt.Errorf("testclient: timeout waiting for %q (got %d bytes)", needle, buf.Len())
		}
	}
}

// AwaitReplayBoundary consumes stream frames until the next
// EventScrollbackReplayDone, returning the bytes received between
// the most recent Begin and the Done. DATA + EVENT share one channel
// to preserve wire order — the Done event would otherwise be picked
// before pending DATA via select, returning empty replays.
func (c *Client) AwaitReplayBoundary(timeout time.Duration) ([]byte, error) {
	var buf bytes.Buffer
	deadline := time.Now().Add(timeout)
	sawBegin := false
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return buf.Bytes(), errors.New("testclient: timeout waiting for replay done")
		}
		select {
		case msg := <-c.stream:
			switch msg.t {
			case wire.FrameData:
				if sawBegin {
					buf.Write(msg.payload)
				}
			case wire.FrameEvent:
				var ev wire.Event
				_ = json.Unmarshal(msg.payload, &ev)
				switch ev.Kind {
				case wire.EventScrollbackReplayBegin:
					sawBegin = true
					buf.Reset()
				case wire.EventScrollbackReplayDone:
					if !sawBegin {
						return buf.Bytes(), errors.New("testclient: Done without Begin")
					}
					return buf.Bytes(), nil
				}
			}
		case err := <-c.errs:
			return buf.Bytes(), err
		case <-time.After(remaining):
			return buf.Bytes(), errors.New("testclient: timeout waiting for replay done")
		}
	}
}

// AwaitSessionEvent reads SESSION_EVENTs until one matches kind (or
// kind == "" matches any), or timeout. The matched event is returned.
func (c *Client) AwaitSessionEvent(kind string, timeout time.Duration) (wire.SessionEvent, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return wire.SessionEvent{}, fmt.Errorf("testclient: timeout waiting for SESSION_EVENT(%s)", kind)
		}
		select {
		case ev := <-c.sessions:
			if kind == "" || ev.Kind == kind {
				return ev, nil
			}
		case err := <-c.errs:
			return wire.SessionEvent{}, err
		case <-time.After(remaining):
			return wire.SessionEvent{}, fmt.Errorf("testclient: timeout waiting for SESSION_EVENT(%s)", kind)
		}
	}
}

// AwaitSessionsSnapshot consumes the next SESSIONS frame.
func (c *Client) AwaitSessionsSnapshot(timeout time.Duration) (wire.SessionsResp, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return wire.SessionsResp{}, errors.New("testclient: timeout waiting for SESSIONS")
		}
		select {
		case msg := <-c.snaps:
			if msg.t != wire.FrameSessions {
				// Drain — control-mode also emits the initial PROJECTS
				// snapshot. Keep going until we see SESSIONS.
				continue
			}
			var resp wire.SessionsResp
			if err := json.Unmarshal(msg.payload, &resp); err != nil {
				return wire.SessionsResp{}, err
			}
			return resp, nil
		case err := <-c.errs:
			return wire.SessionsResp{}, err
		case <-time.After(remaining):
			return wire.SessionsResp{}, errors.New("testclient: timeout waiting for SESSIONS")
		}
	}
}

// DrainInitialSnapshots consumes the unsolicited PROJECTS + SESSIONS
// pair the daemon emits to every control connection after Handshake.
func (c *Client) DrainInitialSnapshots(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	got := 0
	for got < 2 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("testclient: timeout draining initial snapshots")
		}
		select {
		case msg := <-c.snaps:
			if msg.t == wire.FrameProjects || msg.t == wire.FrameSessions {
				got++
			}
		case err := <-c.errs:
			return err
		case <-time.After(remaining):
			return errors.New("testclient: timeout draining initial snapshots")
		}
	}
	return nil
}

// Close shuts down the reader goroutine and closes the connection.
// Safe to call multiple times.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.conn.Close()
	})
	return err
}

func (c *Client) readLoop() {
	for {
		select {
		case <-c.closed:
			return
		default:
		}
		ft, payload, err := wire.ReadFrame(c.conn)
		if err != nil {
			// io.EOF / closed conn is the normal teardown path; only
			// surface unexpected errors.
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				select {
				case c.errs <- err:
				default:
				}
			}
			return
		}
		switch ft {
		case wire.FrameData, wire.FrameEvent:
			c.sendStream(frameMsg{t: ft, payload: payload})
		case wire.FrameSessionEvent:
			var ev wire.SessionEvent
			if err := json.Unmarshal(payload, &ev); err == nil {
				c.sendSessionEvent(ev)
			}
		case wire.FrameProjectEvent:
			var ev wire.ProjectEvent
			if err := json.Unmarshal(payload, &ev); err == nil {
				c.sendProjectEvent(ev)
			}
		default:
			c.sendSnapshot(frameMsg{t: ft, payload: payload})
		}
	}
}

// Buffered-channel sends with closed-check; never block forever.
func (c *Client) sendStream(m frameMsg) {
	select {
	case c.stream <- m:
	case <-c.closed:
	}
}
func (c *Client) sendSessionEvent(v wire.SessionEvent) {
	select {
	case c.sessions <- v:
	case <-c.closed:
	}
}
func (c *Client) sendProjectEvent(v wire.ProjectEvent) {
	select {
	case c.projects <- v:
	case <-c.closed:
	}
}
func (c *Client) sendSnapshot(m frameMsg) {
	select {
	case c.snaps <- m:
	case <-c.closed:
	}
}

// --- Isolation guard ---

// RequireIsolation asserts the caller's environment is safe for
// running an e2e test against a fresh daemon. Specifically: HIVE_SOCKET
// and HIVE_STATE_DIR must both be set and point under a known-temp
// prefix (os.TempDir or /tmp). Tests must call this before spawning a
// daemon — it is the last line of defence against an e2e run touching
// the user's real hive state.
func RequireIsolation() error {
	sock := os.Getenv("HIVE_SOCKET")
	state := os.Getenv("HIVE_STATE_DIR")
	if sock == "" {
		return errors.New("testclient: HIVE_SOCKET is unset; refusing to run (isolation contract)")
	}
	if state == "" {
		return errors.New("testclient: HIVE_STATE_DIR is unset; refusing to run (isolation contract)")
	}
	tmpPrefixes := []string{os.TempDir(), "/tmp", "/private/tmp", "/var/folders"}
	if !hasAnyPrefix(sock, tmpPrefixes) {
		return fmt.Errorf("testclient: HIVE_SOCKET=%q is outside temp; refusing to run", sock)
	}
	if !hasAnyPrefix(state, tmpPrefixes) {
		return fmt.Errorf("testclient: HIVE_STATE_DIR=%q is outside temp; refusing to run", state)
	}
	return nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p+string(os.PathSeparator)) || s == p {
			return true
		}
	}
	return false
}
