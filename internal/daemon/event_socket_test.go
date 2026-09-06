package daemon

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func dialEvents(t *testing.T, d *Daemon) net.Conn {
	t.Helper()
	c, err := net.Dial("unix", d.EventSocketPath())
	if err != nil {
		t.Fatalf("dial events socket: %v", err)
	}
	return c
}

// The events socket sits next to the control socket, in the same
// verified directory, and is not the control socket.
func TestEventSocketPathSitsBesideControl(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	ev := d.EventSocketPath()
	if ev == d.SocketPath() {
		t.Fatal("events socket is the control socket")
	}
	if got, want := filepath.Dir(ev), filepath.Dir(d.SocketPath()); got != want {
		t.Fatalf("events socket dir = %q, want %q", got, want)
	}
}

// The events socket applies a well-formed AGENT_EVENT exactly like the
// control socket's event mode does.
func TestEventSocketAcceptsAgentEvent(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0", Mode: wire.ModeEvent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, wire.AgentEvent{
		SessionID: id,
		Kind:      wire.AgentEventPrompt,
		Source:    wire.StateSourceHook,
		Text:      "say pong",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		info := findSession(d, id)
		return info.State == wire.StateWorking &&
			info.StateSource == wire.StateSourceHook &&
			info.LastPrompt == "say pong"
	})
}

// Every other HELLO mode is refused on the events socket with
// mode_not_allowed, and nothing is created.
func TestEventSocketRefusesControlAndCreate(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	before := len(d.Registry().List())
	for _, mode := range []wire.Mode{wire.ModeControl, wire.ModeCreate, wire.ModeAttach} {
		c := dialEvents(t, d)
		hello := wire.Hello{Version: wire.PROTOCOL_VERSION, Client: "test/0", Mode: mode}
		if mode == wire.ModeCreate {
			hello.Create = &wire.CreateSpec{Cmd: []string{"/bin/sh", "-c", "true"}}
		}
		if err := wire.WriteJSON(c, wire.FrameHello, hello); err != nil {
			t.Fatal(err)
		}
		var e wire.Error
		ft, err := wire.ReadJSON(c, &e)
		if err != nil {
			t.Fatalf("mode %s: read: %v", mode, err)
		}
		if ft != wire.FrameError || e.Code != wire.ErrCodeModeNotAllowed {
			t.Fatalf("mode %s: got %s %+v, want ERROR mode_not_allowed", mode, ft, e)
		}
		_ = c.Close()
	}
	if got := len(d.Registry().List()); got != before {
		t.Fatalf("sessions: %d → %d; create must not succeed on events socket", before, got)
	}
}

// The control socket keeps serving every mode; narrowing the events
// socket must not have narrowed it too.
func TestControlSocketStillServesControl(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := dial(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0", Mode: wire.ModeControl,
	}); err != nil {
		t.Fatal(err)
	}
	var w wire.Welcome
	ft, err := wire.ReadJSON(c, &w)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if ft != wire.FrameWelcome {
		t.Fatalf("got %s, want WELCOME", ft)
	}
}

// The environment handed to a spawned session points at the events
// socket, never the control socket.
func TestSpawnedSessionEnvUsesEventSocket(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	env := d.Registry().HiveEnvForTest("abc")
	want := "HIVE_SOCKET=" + d.EventSocketPath()
	found := false
	for _, kv := range env {
		if kv == want {
			found = true
		}
		if kv == "HIVE_SOCKET="+d.SocketPath() {
			t.Fatalf("env exports the control socket: %v", env)
		}
	}
	if !found {
		t.Fatalf("env %v lacks %q", env, want)
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
