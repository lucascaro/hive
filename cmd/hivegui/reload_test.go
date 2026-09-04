package main

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// stubRelaunch replaces the process-replacement seams for the lifetime
// of the test and reports what they saw.
//
// Seaming is not optional here: spawnNewGUI re-execs os.Executable(),
// which inside a test binary is the test binary. A test that reached
// the real one would spawn a detached copy of the whole suite against
// the developer's real state directory — the same trap update_action's
// restartDaemonFn exists to avoid.
type relaunchStub struct {
	spawned  int
	quit     int
	err      error
	emits    []string
	emitData []string
}

func stubRelaunch(t *testing.T) *relaunchStub {
	t.Helper()
	s := &relaunchStub{}
	prevSpawn, prevQuit := spawnNewGUIFn, quitFn
	spawnNewGUIFn = func(string) error {
		s.spawned++
		return s.err
	}
	quitFn = func(*App) { s.quit++ }
	prevEmit := emitFn
	emitFn = func(_ *App, name string, data ...any) {
		s.emits = append(s.emits, name)
		if len(data) > 0 {
			if str, ok := data[0].(string); ok {
				s.emitData = append(s.emitData, str)
			}
		}
	}
	t.Cleanup(func() {
		spawnNewGUIFn, quitFn, emitFn = prevSpawn, prevQuit, prevEmit
	})
	return s
}

// pipeClient returns a wire.Client wrapping one end of a net.Pipe and
// the raw other end, so a test can assert exactly what the GUI wrote
// to the daemon — including that it wrote nothing.
func pipeClient(t *testing.T) (*wire.Client, net.Conn) {
	t.Helper()
	cli, srv := net.Pipe()
	t.Cleanup(func() { _ = srv.Close() })
	return wire.NewClient(cli), srv
}

func TestReloadGUIRelaunchesAndQuits(t *testing.T) {
	s := stubRelaunch(t)
	a := &App{attaches: map[string]*wire.Client{}}

	if err := a.ReloadGUI(); err != nil {
		t.Fatalf("ReloadGUI: %v", err)
	}
	if s.spawned != 1 || s.quit != 1 {
		t.Errorf("spawned=%d quit=%d, want 1/1", s.spawned, s.quit)
	}
}

// The load-bearing property of the whole feature: a reload must leave
// hived running. Both kill channels are reachable only from
// RestartDaemon, so the assertion is structural — the control
// connection must never see a SHUTDOWN frame.
func TestReloadGUINeverShutsDownTheDaemon(t *testing.T) {
	stubRelaunch(t)

	client, srv := pipeClient(t)
	a := &App{control: client, attaches: map[string]*wire.Client{}}

	if err := a.ReloadGUI(); err != nil {
		t.Fatalf("ReloadGUI: %v", err)
	}

	// relaunchSelf closed the conn, so the daemon side sees EOF rather
	// than a frame. Anything else means a reload wrote to the daemon.
	if ft, _, err := wire.ReadFrame(srv); err == nil {
		t.Fatalf("reload wrote %s to the daemon; it must write nothing at all", ft)
	}
}

func TestReloadGUIClosesAttachConnections(t *testing.T) {
	stubRelaunch(t)

	attach, attachSrv := pipeClient(t)
	a := &App{attaches: map[string]*wire.Client{"s1": attach}}

	if err := a.ReloadGUI(); err != nil {
		t.Fatalf("ReloadGUI: %v", err)
	}
	if len(a.attaches) != 0 {
		t.Errorf("attaches = %d, want 0 — the outgoing process must not hold half-open fds", len(a.attaches))
	}
	if _, _, err := wire.ReadFrame(attachSrv); err == nil {
		t.Error("attach conn still open after reload")
	}
}

// The broadcast reaches every window including the sender, and a user
// can click the menu item twice. Without the latch that is two
// replacement windows for one reload.
func TestReloadIsIdempotentUnderBroadcastStorm(t *testing.T) {
	s := stubRelaunch(t)
	a := &App{attaches: map[string]*wire.Client{}}

	for i := 0; i < 5; i++ {
		if err := a.ReloadGUI(); err != nil {
			t.Fatalf("ReloadGUI #%d: %v", i, err)
		}
	}
	if s.spawned != 1 {
		t.Errorf("spawned %d replacement windows, want exactly 1", s.spawned)
	}
}

// A failed spawn must NOT quit: the user is left in a working window
// with a visible error, rather than in no window at all.
func TestReloadGUIDoesNotQuitWhenSpawnFails(t *testing.T) {
	s := stubRelaunch(t)
	s.err = errors.New("boom")
	a := &App{attaches: map[string]*wire.Client{}}

	if err := a.ReloadGUI(); err == nil {
		t.Fatal("want an error when the replacement cannot be spawned")
	}
	if s.quit != 0 {
		t.Error("quit after a failed spawn: the user would be left with no window at all")
	}
}

func TestHandleClientCommandReloadsOnReloadGUI(t *testing.T) {
	s := stubRelaunch(t)
	a := &App{attaches: map[string]*wire.Client{}}

	payload, _ := json.Marshal(wire.ClientCommand{Cmd: wire.CmdReloadGUI})
	a.handleClientCommand(payload)

	if s.spawned != 1 || s.quit != 1 {
		t.Errorf("spawned=%d quit=%d, want 1/1 — a relayed reload_gui must relaunch this window",
			s.spawned, s.quit)
	}
}

// Anything that is not reload_gui is frontend business. It must not
// reach the relaunch path — a focus_session that relaunched the window
// would be a spectacular way to lose someone's place.
func TestHandleClientCommandDoesNotReloadOnOtherVerbs(t *testing.T) {
	s := stubRelaunch(t)
	a := &App{attaches: map[string]*wire.Client{}}

	payload, _ := json.Marshal(wire.ClientCommand{
		Cmd: wire.CmdFocusSession, SessionID: "s1",
	})
	a.handleClientCommand(payload)

	if s.spawned != 0 || s.quit != 0 {
		t.Errorf("spawned=%d quit=%d, want 0/0", s.spawned, s.quit)
	}
	// It must still reach the frontend, verbatim — hivebar's
	// focus_session has no other route to the window.
	if len(s.emits) != 1 || s.emits[0] != "client:command" {
		t.Fatalf("emits = %v, want one client:command", s.emits)
	}
	if !strings.Contains(s.emitData[0], "s1") {
		t.Errorf("forwarded payload lost the session id: %q", s.emitData[0])
	}
}

func TestHandleClientCommandIgnoresGarbage(t *testing.T) {
	s := stubRelaunch(t)
	a := &App{attaches: map[string]*wire.Client{}}

	a.handleClientCommand([]byte("not json"))

	if s.spawned != 0 {
		t.Error("a malformed relay payload must not relaunch anything")
	}
}

// RequestReloadAllGUIs must go out over the wire rather than reloading
// in place: each window is its own process, so a local reload leaves
// the siblings on the old binary.
func TestRequestReloadAllGUIsSendsTheCommand(t *testing.T) {
	s := stubRelaunch(t)
	client, srv := pipeClient(t)
	a := &App{control: client, attaches: map[string]*wire.Client{}}

	errc := make(chan error, 1)
	go func() { errc <- a.RequestReloadAllGUIs() }()

	var cmd wire.ClientCommand
	ft, err := wire.ReadJSON(srv, &cmd)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("RequestReloadAllGUIs: %v", err)
	}
	if ft != wire.FrameClientCommand || cmd.Cmd != wire.CmdReloadGUI {
		t.Errorf("sent %s/%q, want CLIENT_COMMAND/reload_gui", ft, cmd.Cmd)
	}
	// The request itself must not relaunch: the daemon's broadcast is
	// what does that, so every window moves together.
	if s.spawned != 0 {
		t.Error("RequestReloadAllGUIs relaunched locally instead of asking the daemon")
	}
}
