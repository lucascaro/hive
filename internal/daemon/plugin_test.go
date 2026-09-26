package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/plugin"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// writeSleepPlugin writes a plugin dir whose main command is `sleep`,
// which is all a daemon test needs from a plugin process.
func writeSleepPlugin(t *testing.T, id string) string {
	t.Helper()
	dir := t.TempDir()
	man := plugin.Manifest{ID: id, Name: "Sleep " + id, Version: "1.0.0", APIVersion: plugin.APIVersion,
		Main: &plugin.Entry{Command: []string{"sleep", "3600"}}}
	b, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// controlConn dials the main socket in control mode.
func pluginTestControl(t *testing.T, d *Daemon, client string) net.Conn {
	t.Helper()
	c := dial(t, d)
	t.Cleanup(func() { _ = c.Close() })
	handshake(t, c, wire.Hello{Mode: wire.ModeControl, Client: client})
	return c
}

// pluginConn opens a plugin socket for id (as the Manager would for a
// run) and dials it in control mode, claiming to be client.
func pluginConn(t *testing.T, d *Daemon, id, client string) (net.Conn, string) {
	t.Helper()
	path, closeFn, err := d.listenPlugin(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	handshake(t, c, wire.Hello{Mode: wire.ModeControl, Client: client})
	return c, path
}

func answerers(d *Daemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.controlClients
}

func awaitPluginEvent(t *testing.T, c net.Conn, pred func(wire.PluginEvent) bool) wire.PluginEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var ev wire.PluginEvent
		if err := json.Unmarshal(readControlFrame(t, c, wire.FramePluginEvent, time.Until(deadline)), &ev); err != nil {
			t.Fatal(err)
		}
		if pred(ev) {
			return ev
		}
	}
	t.Fatal("no matching PLUGIN_EVENT")
	return wire.PluginEvent{}
}

func TestPluginOps_BroadcastToAllControlClients(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	a := pluginTestControl(t, d, "hivegui/a")
	b := pluginTestControl(t, d, "hivegui/b")

	src := writeSleepPlugin(t, "sleepy")
	if err := wire.WriteJSON(a, wire.FrameInstallPlugin, wire.InstallPluginReq{Source: src, Nonce: "n-a"}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]net.Conn{"a": a, "b": b} {
		ev := awaitPluginEvent(t, c, func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventAdded })
		if ev.Plugin.ID != "sleepy" || ev.Plugin.Enabled || ev.Nonce != "n-a" {
			t.Fatalf("client %s: added event = %+v", name, ev)
		}
		if len(ev.Plugin.Command) == 0 || ev.Plugin.Command[0] != "sleep" {
			t.Fatalf("client %s: command not reported for consent: %+v", name, ev.Plugin.Command)
		}
	}

	_ = wire.WriteJSON(a, wire.FrameSetPluginEnabled, wire.SetPluginEnabledReq{ID: "sleepy", Enabled: true})
	awaitPluginEvent(t, b, func(ev wire.PluginEvent) bool { return ev.Plugin.Status == wire.PluginRunning })

	_ = wire.WriteJSON(b, wire.FrameListPlugins, struct{}{})
	var list wire.PluginsResp
	_ = json.Unmarshal(readControlFrame(t, b, wire.FramePlugins, 5*time.Second), &list)
	if len(list.Plugins) != 1 || !list.Plugins[0].Enabled {
		t.Fatalf("LIST_PLUGINS = %+v", list)
	}

	_ = wire.WriteJSON(b, wire.FrameRemovePlugin, wire.RemovePluginReq{ID: "sleepy"})
	awaitPluginEvent(t, a, func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventRemoved })
}

func TestInstallNonceEchoedOnError(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/x")
	_ = wire.WriteJSON(c, wire.FrameInstallPlugin, wire.InstallPluginReq{Source: "/no/such/plugin/dir", Nonce: "n-err"})
	var e wire.Error
	_ = json.Unmarshal(readControlFrame(t, c, wire.FrameError, 5*time.Second), &e)
	if e.Code != wire.ErrCodePluginInstallFailed || e.Nonce != "n-err" {
		t.Fatalf("install error = %+v", e)
	}
	_ = wire.WriteJSON(c, wire.FrameRemovePlugin, wire.RemovePluginReq{ID: "ghost"})
	_ = json.Unmarshal(readControlFrame(t, c, wire.FrameError, 5*time.Second), &e)
	if e.Code != wire.ErrCodePluginNotFound {
		t.Fatalf("remove unknown = %+v", e)
	}
}

// A connection is a plugin because of the socket it dialed — not
// because of what it calls itself.
func TestPluginSocket_TaggedRegardlessOfClientName(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	base := answerers(d)
	pluginConn(t, d, "py", "hivegui/pretending")
	time.Sleep(50 * time.Millisecond)
	if n := answerers(d); n != base {
		t.Fatalf("a plugin socket connection counted as an answerer (%d → %d)", base, n)
	}
	pluginTestControl(t, d, "python")
	deadline := time.Now().Add(2 * time.Second)
	for answerers(d) != base+1 {
		if time.Now().After(deadline) {
			t.Fatalf("a main-socket client was not counted (answerers %d)", answerers(d))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPluginSocket_RefusesInSessionModes(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	path, closeFn, err := d.listenPlugin("modes")
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	for _, mode := range []wire.Mode{wire.ModeEvent, wire.ModeSession, wire.ModePlanReview} {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		_ = wire.WriteJSON(c, wire.FrameHello, wire.Hello{Version: wire.PROTOCOL_VERSION, Mode: mode})
		var e wire.Error
		ft, err := wire.ReadJSON(c, &e)
		c.Close()
		if err != nil || ft != wire.FrameError || e.Code != wire.ErrCodeModeNotAllowed {
			t.Errorf("mode %s on a plugin socket: %s %+v %v", mode, ft, e, err)
		}
	}
}

// roundTrips runs LIST_SESSIONS request/reply pairs on c for dur and
// returns the achieved rate.
func roundTrips(t *testing.T, c net.Conn, dur time.Duration) float64 {
	t.Helper()
	n := 0
	start := time.Now()
	for time.Since(start) < dur {
		if err := wire.WriteJSON(c, wire.FrameListSessions, struct{}{}); err != nil {
			t.Fatal(err)
		}
		readControlFrame(t, c, wire.FrameSessions, 5*time.Second)
		n++
	}
	return float64(n) / time.Since(start).Seconds()
}

func shrinkPluginBudget(t *testing.T, rate, burst float64) {
	t.Helper()
	oRate, oBurst := plugin.RatePerSec, plugin.Burst
	plugin.RatePerSec, plugin.Burst = rate, burst
	t.Cleanup(func() { plugin.RatePerSec, plugin.Burst = oRate, oBurst })
}

func TestPluginSocket_RateLimited(t *testing.T) {
	skipOnWindows(t)
	shrinkPluginBudget(t, 200, 20)
	d := startTestDaemon(t)
	pc, _ := pluginConn(t, d, "busy", "p")
	roundTrips(t, pc, 200*time.Millisecond) // drain the burst
	got := roundTrips(t, pc, 2*time.Second)
	if got > 220 {
		t.Fatalf("plugin achieved %.0f LIST_SESSIONS/s; budget is 200/s", got)
	}
	// The same loop on an ordinary connection is not throttled — which
	// is what makes the assertion above able to fail.
	gc := pluginTestControl(t, d, "hivegui/x")
	if free := roundTrips(t, gc, time.Second); free < 2*220 {
		t.Fatalf("unthrottled client managed only %.0f/s; the rate test proves nothing", free)
	}
}

func TestPluginSocket_CreateModeCharged(t *testing.T) {
	skipOnWindows(t)
	shrinkPluginBudget(t, 1000, 100) // create costs 100: 10/s, burst of 1
	d := startTestDaemon(t)
	d.createFn = func(context.Context, wire.CreateSpec) (*registry.Entry, error) {
		return nil, errors.New("test: no create")
	}
	path, closeFn, err := d.listenPlugin("spawner")
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	create := func() {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = wire.WriteJSON(c, wire.FrameHello, wire.Hello{Version: wire.PROTOCOL_VERSION, Mode: wire.ModeCreate})
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _, _ = wire.ReadFrame(c)
	}
	create() // spend the burst
	n := 0
	start := time.Now()
	for time.Since(start) < 1500*time.Millisecond {
		create()
		n++
	}
	if rate := float64(n) / time.Since(start).Seconds(); rate > 11 {
		t.Fatalf("plugin created at %.1f/s via create-mode HELLO; budget is 10/s", rate)
	}
}

func TestPluginSocket_FloodFanoutOtherClientStillGetsEvents(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	projectID := d.Registry().ListProjects()[0].ID
	sid := firstSessionID(t, d)
	gui := pluginTestControl(t, d, "hivegui/x")
	pc, _ := pluginConn(t, d, "flood", "p")

	stopFlood := make(chan struct{})
	defer close(stopFlood)
	go func() { // drain whatever the plugin is sent
		buf := make([]byte, 64<<10)
		for {
			if _, err := pc.Read(buf); err != nil {
				return
			}
		}
	}()
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stopFlood:
				return
			default:
			}
			if wire.WriteJSON(pc, wire.FrameAddIdea, wire.AddIdeaReq{ProjectID: projectID, Text: "flood " + strconv.Itoa(i)}) != nil {
				return
			}
		}
	}()
	// The GUI reads continuously, as a real one does; a client that stops
	// reading is (correctly) hung up on, which is not what this measures.
	seen := make(chan time.Time, 1)
	name := "renamed-during-flood"
	go func() {
		for {
			ft, payload, err := wire.ReadFrame(gui)
			if err != nil {
				return
			}
			if ft != wire.FrameSessionEvent {
				continue
			}
			var ev wire.SessionEvent
			if json.Unmarshal(payload, &ev) == nil && ev.Session.Name == name {
				seen <- time.Now()
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	_ = wire.WriteJSON(gui, wire.FrameUpdateSession, wire.UpdateSessionReq{SessionID: sid, Name: &name})
	select {
	case at := <-seen:
		if d := at.Sub(start); d > 500*time.Millisecond {
			t.Fatalf("GUI waited %s for its own event while a plugin flooded", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GUI never received its SESSION_EVENT during the flood (was it hung up on?)")
	}
}

func TestPluginSocket_ThrottledConnReleasedOnClose(t *testing.T) {
	skipOnWindows(t)
	shrinkPluginBudget(t, 1, 1)
	d := startTestDaemon(t)
	pc, _ := pluginConn(t, d, "slow", "p")
	for range 5 { // well past the budget: the read loop is now asleep
		_ = wire.WriteJSON(pc, wire.FrameListSessions, struct{}{})
	}
	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = d.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked on a throttled plugin connection")
	}
}

// A plugin gets the dispatch a GUI gets: the same verbs, the same
// replies, the same broadcasts, and input through attach.
func TestPluginSocket_ServesSameDispatch(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	sid := firstSessionID(t, d)
	projectID := d.Registry().ListProjects()[0].ID
	pc, path := pluginConn(t, d, "full", "p")

	send := func(ft wire.FrameType, v any) {
		t.Helper()
		if err := wire.WriteJSON(pc, ft, v); err != nil {
			t.Fatal(err)
		}
	}
	send(wire.FrameListSessions, struct{}{})
	readControlFrame(t, pc, wire.FrameSessions, 5*time.Second)
	send(wire.FrameListPlugins, struct{}{})
	readControlFrame(t, pc, wire.FramePlugins, 5*time.Second)

	send(wire.FrameCreateProject, wire.CreateProjectReq{Name: "from-plugin", Cwd: t.TempDir()})
	readControlFrame(t, pc, wire.FrameProjectEvent, 5*time.Second)

	send(wire.FrameAddIdea, wire.AddIdeaReq{ProjectID: projectID, Text: "plugin idea"})
	readControlFrame(t, pc, wire.FrameIdeaEvent, 5*time.Second)

	send(wire.FrameClientCommand, wire.ClientCommand{Cmd: wire.CmdFocusSession, SessionID: sid})
	readControlFrame(t, pc, wire.FrameClientBroadcast, 5*time.Second)

	name := "renamed-by-plugin"
	send(wire.FrameUpdateSession, wire.UpdateSessionReq{SessionID: sid, Name: &name})
	awaitSessionEvent(t, pc, func(ev wire.SessionEvent) bool { return ev.Session.Name == name }, 5*time.Second)

	// Input goes through attach on the same plugin socket.
	ac, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ac.Close()
	handshake(t, ac, wire.Hello{Mode: wire.ModeAttach, SessionID: sid})
	if err := wire.WriteFrame(ac, wire.FrameData, []byte("echo plugin-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "plugin-42") && time.Now().Before(deadline) {
		drainFor(ac, &out, 200*time.Millisecond)
	}
	if !strings.Contains(out.String(), "plugin-42") {
		t.Fatalf("attach input from a plugin produced no output; got %q", out.String())
	}
}

// Shutdown cancels an install that is still cloning, including a
// grandchild git forked, instead of waiting out the clone timeout.
func TestDaemonClose_CancelsInflightGitInstall(t *testing.T) {
	skipOnWindows(t)
	if runtime.GOOS == "windows" {
		return
	}
	shim := shortTempDir(t)
	pidFile := filepath.Join(shim, "child.pid")
	script := "#!/bin/sh\nsleep 300 &\necho $! > " + pidFile + "\nwait\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/x")
	_ = wire.WriteJSON(c, wire.FrameInstallPlugin, wire.InstallPluginReq{Source: "https://example.invalid/p.git"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("git shim never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	_ = d.Close()
	if dur := time.Since(start); dur > 8*time.Second {
		t.Fatalf("Close took %s with a clone in flight", dur)
	}
}

// A control socket path near the OS limit leaves no room for a plugin
// socket's suffix; that must be a clear refusal, not bind's EINVAL.
func TestListenPlugin_RefusesOverlongPath(t *testing.T) {
	d := &Daemon{sock: "/" + strings.Repeat("x", maxSockPath()-12), stop: make(chan struct{})}
	_, _, err := d.listenPlugin("long")
	if err == nil || !strings.Contains(err.Error(), "HIVE_SOCKET") {
		t.Fatalf("listenPlugin on an overlong path = %v, want a refusal naming HIVE_SOCKET", err)
	}
}
