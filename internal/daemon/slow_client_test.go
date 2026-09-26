package daemon

import (
	"bytes"
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// setLimits shrinks the client-write bounds for one test. Sinks and
// control conns capture them when they are created, so they must be
// set before the client under test connects.
func setLimits(t *testing.T, timeout time.Duration, backlog int) {
	t.Helper()
	oldT, oldL := clientWriteTimeout.Load(), attachBacklogLimit.Load()
	clientWriteTimeout.Store(int64(timeout))
	attachBacklogLimit.Store(int64(backlog))
	t.Cleanup(func() {
		clientWriteTimeout.Store(oldT)
		attachBacklogLimit.Store(oldL)
	})
}

// attachAndStall attaches to id and never reads again: the client that
// "stopped reading its socket" from #461.
func attachAndStall(t *testing.T, d *Daemon, id string) net.Conn {
	t.Helper()
	c := dial(t, d)
	t.Cleanup(func() { _ = c.Close() })
	handshake(t, c, wire.Hello{Mode: wire.ModeAttach, SessionID: id})
	return c
}

// attachReading attaches to id and consumes the initial replay.
func attachReading(t *testing.T, d *Daemon, id string) net.Conn {
	t.Helper()
	c := dial(t, d)
	t.Cleanup(func() { _ = c.Close() })
	handshake(t, c, wire.Hello{Mode: wire.ModeAttach, SessionID: id})
	var sink bytes.Buffer
	readUntilReplayDone(t, c, &sink)
	return c
}

// floodCmd prints far more than the backlog limit plus both socket
// buffers, then a marker. The marker is split in the typed command
// (EN""D) so the shell's echo of the line cannot match it.
const floodCmd = "head -c 4000000 /dev/zero | tr '\\0' x; echo; echo EN\"\"D_MARK\n"

// awaitMarker reads c until the output contains marker.
func awaitMarker(t *testing.T, c net.Conn, marker string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	var tail []byte
	for {
		_ = c.SetReadDeadline(deadline)
		ft, p, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("never saw %q: %v (the session's output is stalled)", marker, err)
		}
		if ft != wire.FrameData {
			continue
		}
		tail = append(tail, p...)
		if bytes.Contains(tail, []byte(marker)) {
			return
		}
		if len(tail) > 64 {
			tail = tail[len(tail)-64:]
		}
	}
}

// awaitHangUp reads c until it errors and requires that error to be the
// daemon hanging up, not the read deadline.
func awaitHangUp(t *testing.T, c net.Conn, within time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(within))
	buf := make([]byte, 64<<10)
	for {
		_, err := c.Read(buf)
		if err == nil {
			continue
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatalf("the daemon never hung up on a client that stopped reading")
		}
		return
	}
}

// One attached client that stops reading must not stall the session.
// Before #461 the session's deliver wrote to every sink synchronously
// under Session.mu, so a full socket on client A blocked the PTY drain
// and client B never saw another byte.
func TestStalledAttachClientDoesNotStallSession(t *testing.T) {
	skipOnWindows(t)
	// A long timeout, so only the backlog bound can rescue the session.
	setLimits(t, time.Hour, 256<<10)
	d := startTestDaemon(t)
	id := firstSessionID(t, d)

	stalled := attachAndStall(t, d, id)
	reader := attachReading(t, d, id)

	if err := wire.WriteFrame(reader, wire.FrameData, []byte(floodCmd)); err != nil {
		t.Fatalf("type flood command: %v", err)
	}
	awaitMarker(t, reader, "END_MARK", 20*time.Second)
	awaitHangUp(t, stalled, 10*time.Second)
}

// While client A is stalled, another client must still be able to
// attach. Before #461 SubscribeWithAtomicReplay waited on the Session.mu
// that A's blocked write held.
func TestStalledAttachClientDoesNotBlockNewAttach(t *testing.T) {
	skipOnWindows(t)
	setLimits(t, time.Hour, 256<<10)
	d := startTestDaemon(t)
	id := firstSessionID(t, d)

	stalled := attachAndStall(t, d, id)
	if err := wire.WriteFrame(stalled, wire.FrameData, []byte(floodCmd)); err != nil {
		t.Fatalf("type flood command: %v", err)
	}
	// Give the flood time to fill A's socket.
	time.Sleep(500 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		c := dial(t, d)
		defer c.Close()
		handshake(t, c, wire.Hello{Mode: wire.ModeAttach, SessionID: id})
		var sink bytes.Buffer
		readUntilReplayDone(t, c, &sink)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a new attach hung behind a stalled client")
	}
}

// A control client that stops reading is hung up on within the write
// timeout, and its serveControl returns instead of parking forever.
func TestStalledControlClientIsDisconnected(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	setLimits(t, 200*time.Millisecond, int(attachBacklogLimit.Load()))

	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		d.serveControl(ctx, server, wire.Hello{Mode: wire.ModeControl}, nil)
		close(done)
	}()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	if ft, _, err := wire.ReadFrame(client); err != nil || ft != wire.FrameWelcome {
		t.Fatalf("read WELCOME: %s %v", ft, err)
	}
	// Stop reading. The snapshot write blocks on the unbuffered pipe.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serveControl is still parked on a client that stopped reading")
	}
	awaitHangUp(t, client, 5*time.Second)
}

// A subscription the daemon drops (the client fell behind) must end the
// connection. Before #461 the fan-out goroutine returned and the
// connection stayed up: the client kept getting replies but never
// another event, with nothing to tell it so.
func TestDroppedSubscriptionClosesControlConn(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	// No deadline rescue: only the dropped subscription can end this.
	setLimits(t, time.Hour, int(attachBacklogLimit.Load()))

	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.serveControl(ctx, server, wire.Hello{Mode: wire.ModeControl}, nil)
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	if ft, _, err := wire.ReadFrame(client); err != nil || ft != wire.FrameWelcome {
		t.Fatalf("read WELCOME: %s %v", ft, err)
	}
	// The fan-out is now parked writing the snapshot. Overflow its
	// client-command subscription (buffer 8): the hub drops and closes it.
	for i := 0; i < 20; i++ {
		d.commands.Publish(wire.ClientCommand{Cmd: wire.CmdReloadGUI})
	}
	// Resume reading: the buffered frames arrive, then the hang-up.
	awaitHangUp(t, client, 5*time.Second)
}

// Close must not wait on an op that is blocked replying to a client
// that stopped reading. Before #461 Close waited on d.ops before
// closing client conns, and that conn close was the only thing that
// could unblock the op.
func TestCloseDoesNotHangOnStuckClientReply(t *testing.T) {
	skipOnWindows(t)
	// No deadline rescue: only closing the conn first can free the op.
	setLimits(t, time.Hour, int(attachBacklogLimit.Load()))
	tmp := shortTempDir(t)
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Through serve, so the conn is tracked in d.clients like a real one.
	go d.serve(ctx, server)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	handshake(t, client, wire.Hello{Mode: wire.ModeControl})
	// The fan-out now blocks writing the snapshot (nobody reads), holding
	// connMu. This request's runOp queues behind it and pins d.ops.
	if err := wire.WriteJSON(client, wire.FrameListWorktrees, wire.ListWorktreesReq{ProjectID: "nope"}); err != nil {
		t.Fatalf("send LIST_WORKTREES: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		_ = d.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Daemon.Close hung on an op blocked writing to a stuck client")
	}
}

// Once Close has drained d.ops, runOp must refuse new work: an ops.Add
// after Close's ops.Wait is WaitGroup misuse, and the op would outlive
// the daemon.
func TestRunOpAfterCloseIsDropped(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ran := make(chan struct{})
	d.runOp(func() { close(ran) })
	d.ops.Wait() // must not block on the dropped op
	select {
	case <-ran:
		t.Fatal("runOp ran fn after Close")
	case <-time.After(100 * time.Millisecond):
	}
}

// Handshake writes get their deadline immediately before the write, not
// at accept: ModeCreate creates the session (a synchronous `git
// worktree add` in real use) before its WELCOME, and a deadline armed
// at accept would expire under a slow create.
func TestSlowCreateStillGetsWelcome(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	setLimits(t, 100*time.Millisecond, int(attachBacklogLimit.Load()))
	d.createFn = func(ctx context.Context, spec wire.CreateSpec) (*registry.Entry, error) {
		time.Sleep(300 * time.Millisecond)
		return d.reg.Create(ctx, spec)
	}

	c := dial(t, d)
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	w := handshake(t, c, wire.Hello{Mode: wire.ModeCreate, Create: &wire.CreateSpec{Shell: "/bin/sh"}})
	if w.SessionID == "" {
		t.Fatal("WELCOME carried no session id")
	}
}
