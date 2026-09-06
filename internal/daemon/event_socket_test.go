package daemon

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/registry"
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

// The idea verbs on a session connection are bound to the caller's own
// session and project. Allowlisting the verbs is not enough on its own:
// the registry reads an empty ProjectID as "every project".
func TestSessionModeIdeasAreScopedToOwnProject(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	own := projectOfSession(d.Registry().List(), id)
	if own == "" {
		t.Fatal("bootstrap session has no project")
	}
	other, err := d.Registry().CreateProject(wire.CreateProjectReq{Name: "other", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}
	if _, err := d.Registry().AddIdea(registry.IdeaSpec{
		ProjectID: other.ID, Text: "a secret from another project",
	}); err != nil {
		t.Fatalf("seed foreign idea: %v", err)
	}

	c := sessionConn(t, d, id)
	defer c.Close()

	// LIST_IDEAS across every project — what `hive idea list --all`
	// sends — is refused, not silently narrowed.
	if err := wire.WriteJSON(c, wire.FrameListIdeas, wire.ListIdeasReq{}); err != nil {
		t.Fatal(err)
	}
	if err := awaitModeNotAllowed(t, c); err != nil {
		t.Fatalf("LIST_IDEAS{}: %v", err)
	}
	// And so is another project's id by name.
	if err := wire.WriteJSON(c, wire.FrameListIdeas, wire.ListIdeasReq{ProjectID: other.ID}); err != nil {
		t.Fatal(err)
	}
	if err := awaitModeNotAllowed(t, c); err != nil {
		t.Fatalf("LIST_IDEAS{other}: %v", err)
	}

	// A write naming another project lands in the caller's own instead
	// of the one it asked for, and carries the caller's session id
	// rather than the forged one.
	if err := wire.WriteJSON(c, wire.FrameAddIdea, wire.AddIdeaReq{
		ProjectID: other.ID, SessionID: "forged", Text: "planted",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		for _, i := range d.Registry().ListIdeas("") {
			if i.Text == "planted" {
				return i.ProjectID == own && i.SourceSessionID == id
			}
		}
		return false
	})
	for _, i := range d.Registry().ListIdeas(other.ID) {
		if i.Text == "planted" {
			t.Fatal("ADD_IDEA wrote into another project")
		}
	}
}

// A restricted connection is not fed the session, project or broadcast
// streams, and sees only its own project's idea events.
func TestSessionModeReceivesNoForeignEvents(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	other, err := d.Registry().CreateProject(wire.CreateProjectReq{Name: "other", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	c := sessionConn(t, d, id)
	defer c.Close()

	// Cause traffic on every stream a control connection would see.
	if _, err := d.Registry().AddIdea(registry.IdeaSpec{
		ProjectID: other.ID, Text: "foreign idea",
	}); err != nil {
		t.Fatal(err)
	}
	renamed := "renamed"
	if _, err := d.Registry().Update(wire.UpdateSessionReq{SessionID: id, Name: &renamed}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// Then something we ARE entitled to, as a sync point: if it
	// arrives and nothing before it did, the suppression holds.
	if err := wire.WriteJSON(c, wire.FrameAddIdea, wire.AddIdeaReq{Text: "mine"}); err != nil {
		t.Fatal(err)
	}

	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		switch ft {
		case wire.FrameSessionEvent, wire.FrameProjectEvent, wire.FrameClientBroadcast:
			t.Fatalf("restricted connection received %s", ft)
		case wire.FrameIdeaEvent:
			var ev wire.IdeaEvent
			if err := jsonUnmarshal(payload, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Idea.Text == "foreign idea" {
				t.Fatal("restricted connection received another project's IDEA_EVENT")
			}
			if ev.Idea.Text == "mine" {
				return
			}
		}
	}
}

// Two daemons must not share one state directory: both would revive
// every persisted session and fork a second PTY for each.
func TestNewRefusesASecondDaemonOnOneStateDir(t *testing.T) {
	skipOnWindows(t)
	// Shrink the retry budget: this test is about the refusal, and the
	// production budget would make it sit here for five seconds.
	orig := stateLockBudget
	stateLockBudget = 200 * time.Millisecond
	t.Cleanup(func() { stateLockBudget = orig })

	tmp := shortTempDir(t)
	state := filepath.Join(tmp, "state")
	first, err := New(Config{SocketPath: filepath.Join(tmp, "s1"), StateDir: state})
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	defer first.Close()

	// A different socket path, deliberately: this is the upgrade
	// window the socket-file guard cannot see.
	second, err := New(Config{SocketPath: filepath.Join(tmp, "s2"), StateDir: state})
	if err == nil {
		_ = second.Close()
		t.Fatal("New accepted a second daemon on a state dir already in use")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("New error = %v, want ErrAlreadyRunning", err)
	}

	// And the lock is released on teardown, so a replacement can start.
	_ = first.Close()
	third, err := New(Config{SocketPath: filepath.Join(tmp, "s3"), StateDir: state})
	if err != nil {
		t.Fatalf("New after the first daemon closed: %v", err)
	}
	_ = third.Close()
}

// sessionConn opens a ModeSession connection on the events socket and
// reads past the handshake and the narrowed snapshot.
func sessionConn(t *testing.T, d *Daemon, sessionID string) net.Conn {
	t.Helper()
	c := dialEvents(t, d)
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	var w wire.Welcome
	if ft, err := wire.ReadJSON(c, &w); err != nil || ft != wire.FrameWelcome {
		t.Fatalf("handshake: %s %v", ft, err)
	}
	var snap wire.SessionsResp
	if _, err := wire.ReadJSON(c, &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return c
}

// A ModeSession HELLO whose session id resolves to nothing — a stale
// HIVE_SESSION_ID from a copied environment — gets the idea verbs
// refused rather than falling through to an unscoped registry call.
func TestSessionModeUnknownSessionRefusesIdeaVerbs(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := sessionConn(t, d, "no-such-session")
	defer c.Close()

	before := len(d.Registry().ListIdeas(""))
	for _, f := range []struct {
		frame wire.FrameType
		body  any
	}{
		{wire.FrameListIdeas, wire.ListIdeasReq{}},
		{wire.FrameAddIdea, wire.AddIdeaReq{Text: "from nowhere"}},
	} {
		if err := wire.WriteJSON(c, f.frame, f.body); err != nil {
			t.Fatalf("%s: %v", f.frame, err)
		}
		if err := awaitModeNotAllowed(t, c); err != nil {
			t.Fatalf("%s: %v", f.frame, err)
		}
	}
	if got := len(d.Registry().ListIdeas("")); got != before {
		t.Fatalf("ideas: %d → %d; an unresolvable session must not be able to file one", before, got)
	}
}

// A ModeSession connection that goes quiet is hung up on; a control
// connection, which is idle by design between user actions, is not.
func TestSessionModeIdleDeadlineHangsUpQuietConnections(t *testing.T) {
	skipOnWindows(t)
	orig := sessionModeIdleDeadline
	sessionModeIdleDeadline = 150 * time.Millisecond
	t.Cleanup(func() { sessionModeIdleDeadline = orig })

	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	quiet := sessionConn(t, d, id)
	defer quiet.Close()
	_ = quiet.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := wire.ReadFrame(quiet); err == nil {
		t.Fatal("idle session connection was not hung up on")
	}

	// The control socket is exempt: same silence, still connected.
	ctl := dial(t, d)
	defer ctl.Close()
	if err := wire.WriteJSON(ctl, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0", Mode: wire.ModeControl,
	}); err != nil {
		t.Fatal(err)
	}
	var w wire.Welcome
	if _, err := wire.ReadJSON(ctl, &w); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * sessionModeIdleDeadline)
	if err := wire.WriteJSON(ctl, wire.FrameListSessions, wire.ListSessionsReq{}); err != nil {
		t.Fatalf("control connection was hung up on: %v", err)
	}
	_ = ctl.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		ft, _, err := wire.ReadFrame(ctl)
		if err != nil {
			t.Fatalf("control connection went quiet: %v", err)
		}
		if ft == wire.FrameSessions {
			return
		}
	}
}

// The restart handoff: the outgoing daemon holds the lock until its
// teardown finishes, and the replacement the GUI spawns into that gap
// must wait rather than exit.
func TestStateLockWaitsOutAClosingDaemon(t *testing.T) {
	skipOnWindows(t)
	orig := stateLockBudget
	stateLockBudget = 3 * time.Second
	t.Cleanup(func() { stateLockBudget = orig })

	tmp := shortTempDir(t)
	state := filepath.Join(tmp, "state")
	first, err := New(Config{SocketPath: filepath.Join(tmp, "s1"), StateDir: state})
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = first.Close()
	}()

	second, err := New(Config{SocketPath: filepath.Join(tmp, "s2"), StateDir: state})
	if err != nil {
		t.Fatalf("replacement daemon gave up on a lock the outgoing one was about to release: %v", err)
	}
	_ = second.Close()
}
