package daemon

import (
	"fmt"
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

// ModeSession on the events socket serves the idea verbs — this is
// what `hive idea` inside a session speaks — and narrows the SESSIONS
// snapshot to the caller's own entry.
func TestEventSocketSessionModeServesIdeas(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id,
	}); err != nil {
		t.Fatal(err)
	}
	var w wire.Welcome
	if ft, err := wire.ReadJSON(c, &w); err != nil || ft != wire.FrameWelcome {
		t.Fatalf("handshake: %s %v", ft, err)
	}
	// The unprompted snapshot carries this session and no other.
	var snap wire.SessionsResp
	ft, err := wire.ReadJSON(c, &snap)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if ft != wire.FrameSessions {
		t.Fatalf("first frame after WELCOME = %s, want SESSIONS (no PROJECTS for a session conn)", ft)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].ID != id {
		t.Fatalf("snapshot = %+v, want only session %s", snap.Sessions, id)
	}
	if err := wire.WriteJSON(c, wire.FrameAddIdea, wire.AddIdeaReq{
		SessionID: id, Text: "an idea from inside the session",
	}); err != nil {
		t.Fatal(err)
	}
	// Bound the wait: without a deadline a regression that stops
	// IDEA_EVENT reaching a restricted connection blocks here forever
	// and the whole package dies on go test's timeout instead of
	// failing this one test by name.
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	for {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("read after ADD_IDEA: %v", err)
		}
		if ft == wire.FrameError {
			t.Fatalf("ADD_IDEA refused on a session connection: %s", payload)
		}
		if ft == wire.FrameIdeaEvent {
			var ev wire.IdeaEvent
			if err := jsonUnmarshal(payload, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Kind == wire.IdeaEventAdded && ev.Idea.Text == "an idea from inside the session" {
				return
			}
		}
	}
}

// Everything outside the idea verbs is refused on a session connection,
// including the ones that would spawn or destroy work.
func TestSessionModeRefusesEveryOtherVerb(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id,
	}); err != nil {
		t.Fatal(err)
	}
	var w wire.Welcome
	if _, err := wire.ReadJSON(c, &w); err != nil {
		t.Fatal(err)
	}
	before := len(d.Registry().List())
	for _, tc := range []struct {
		frame wire.FrameType
		body  any
	}{
		{wire.FrameCreateSession, wire.CreateSpec{Cmd: []string{"/bin/sh", "-c", "true"}}},
		{wire.FrameKillSession, wire.KillSessionReq{SessionID: id}},
		{wire.FrameListSessions, wire.ListSessionsReq{}},
		{wire.FrameShutdown, struct{}{}},
	} {
		if err := wire.WriteJSON(c, tc.frame, tc.body); err != nil {
			t.Fatalf("%s: write: %v", tc.frame, err)
		}
		if err := awaitModeNotAllowed(t, c); err != nil {
			t.Fatalf("%s: %v", tc.frame, err)
		}
	}
	// The daemon is still up (SHUTDOWN was refused, not obeyed) and
	// nothing was created or killed.
	if got := len(d.Registry().List()); got != before {
		t.Fatalf("sessions: %d → %d", before, got)
	}
	if err := wire.WriteJSON(c, wire.FrameListIdeas, wire.ListIdeasReq{}); err != nil {
		t.Fatalf("daemon went away after a refused SHUTDOWN: %v", err)
	}
}

// awaitModeNotAllowed reads past the snapshot/event traffic to the next
// ERROR frame and checks its code.
func awaitModeNotAllowed(t *testing.T, c net.Conn) error {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	for {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			return err
		}
		if ft != wire.FrameError {
			continue
		}
		var e wire.Error
		if err := jsonUnmarshal(payload, &e); err != nil {
			return err
		}
		if e.Code != wire.ErrCodeModeNotAllowed {
			return fmt.Errorf("error code = %q, want mode_not_allowed", e.Code)
		}
		return nil
	}
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
