package daemon

import (
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// bootstrapSessionID returns the id of the daemon's one bootstrap
// session, so event-mode tests have a real session to target.
func bootstrapSessionID(t *testing.T, d *Daemon) string {
	t.Helper()
	list := d.Registry().List()
	if len(list) == 0 {
		t.Fatalf("no bootstrap session")
	}
	return list[0].ID
}

func dialEvent(t *testing.T, d *Daemon) net.Conn {
	t.Helper()
	c := dial(t, d)
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0", Mode: wire.ModeEvent,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	return c
}

// TestEventModeAcceptsOneFrame: a well-formed AGENT_EVENT is applied to
// the named session's state, and the connection is closed by the
// daemon afterwards (no reply of any kind).
func TestEventModeAcceptsOneFrame(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dialEvent(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, wire.AgentEvent{
		SessionID: id,
		Kind:      wire.AgentEventPrompt,
		Source:    wire.StateSourceHook,
		Text:      "say pong",
	}); err != nil {
		t.Fatalf("write agent event: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		info := findSession(d, id)
		if info.State == wire.StateWorking && info.StateSource == wire.StateSourceHook && info.LastPrompt == "say pong" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session state = %+v, want working/hook/\"say pong\"", info)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func findSession(d *Daemon, id string) wire.SessionInfo {
	for _, s := range d.Registry().List() {
		if s.ID == id {
			return s
		}
	}
	return wire.SessionInfo{}
}

// TestEventModeRejectsControlFrame: any frame type other than
// AGENT_EVENT is refused (the connection is simply closed, no error
// frame — the hook has nothing useful to do with one).
func TestEventModeRejectsControlFrame(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := dialEvent(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameListSessions, wire.ListSessionsReq{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertConnClosed(t, c)
}

// TestEventModeUnknownSessionDropped: an AGENT_EVENT naming a session
// id the registry doesn't know is a silent no-op — no panic, no error
// frame, connection just closes.
func TestEventModeUnknownSessionDropped(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := dialEvent(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, wire.AgentEvent{
		SessionID: "does-not-exist",
		Kind:      wire.AgentEventPing,
		Source:    wire.StateSourceHook,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertConnClosed(t, c)
}

// TestEventModeMalformedJSONDropped: a payload that doesn't even parse
// as AgentEvent's shape closes the connection rather than panicking.
func TestEventModeMalformedJSONDropped(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := dialEvent(t, d)
	defer c.Close()
	if err := wire.WriteFrame(c, wire.FrameAgentEvent, []byte(`["not","an","object"]`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertConnClosed(t, c)
}

// TestEventModeUnknownKindDropped: a kind outside wire.AgentEventKinds
// is refused rather than applied.
func TestEventModeUnknownKindDropped(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := dialEvent(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, wire.AgentEvent{
		SessionID: id, Kind: "bogus", Source: wire.StateSourceHook,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertConnClosed(t, c)
	// And the session's state must be untouched.
	if info := findSession(d, id); info.State != wire.StateIdle {
		t.Errorf("state = %q, want idle (unchanged)", info.State)
	}
}

// TestEventModeReadDeadline: a client that connects in event mode and
// never sends a frame gets dropped within the read deadline, not held
// forever.
func TestEventModeReadDeadline(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := dialEvent(t, d)
	defer c.Close()
	assertConnClosed(t, c)
}

// assertConnClosed reads from c and expects EOF (or any error) within
// a few seconds — proof the daemon closed its side.
func assertConnClosed(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	_, err := c.Read(buf)
	if err == nil {
		t.Fatalf("expected connection to be closed, got a byte instead")
	}
}
