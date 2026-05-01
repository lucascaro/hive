package daemon

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// shortTempDir creates a temp dir under /tmp (short path) so that the
// Unix-socket path stays under macOS's 104-character sun_path limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon E2E test requires POSIX shell")
	}
}

// startTestDaemon brings up a daemon on a temp socket and returns a
// teardown func.
func startTestDaemon(t *testing.T) (*Daemon, context.CancelFunc) {
	t.Helper()
	// Short socket name to stay under macOS's 104-char sun_path limit.
	sock := filepath.Join(shortTempDir(t), "s")
	d, err := New(Config{
		SocketPath: sock,
		Session: session.Options{
			Shell: "/bin/bash",
			Cols:  80,
			Rows:  24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = d.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = d.Close()
	})
	return d, cancel
}

// readUntilReplayDone reads frames until EVENT scrollback_replay_done,
// accumulating any DATA bytes into out.
func readUntilReplayDone(t *testing.T, conn net.Conn, out *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		switch ft {
		case wire.FrameData:
			out.Write(payload)
		case wire.FrameEvent:
			// Treat any event as "replay done" for this helper.
			return
		default:
			t.Fatalf("unexpected frame during replay: %s", ft)
		}
	}
}

// drainFor reads frames into out for the given duration.
func drainFor(conn net.Conn, out *bytes.Buffer, d time.Duration) {
	deadline := time.Now().Add(d)
	for {
		_ = conn.SetReadDeadline(deadline)
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return
		}
		if ft == wire.FrameData {
			out.Write(payload)
		}
	}
}

func handshake(t *testing.T, conn net.Conn) wire.Welcome {
	t.Helper()
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "test/0",
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var w wire.Welcome
	ft, err := wire.ReadJSON(conn, &w)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if ft != wire.FrameWelcome {
		t.Fatalf("expected WELCOME, got %s", ft)
	}
	return w
}

func TestDaemonAttachReattachReplay(t *testing.T) {
	skipOnWindows(t)
	d, _ := startTestDaemon(t)

	dial := func() net.Conn {
		c, err := net.Dial("unix", d.SocketPath())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}

	// First attach: handshake, send a marker command, drain, disconnect.
	c1 := dial()
	w1 := handshake(t, c1)
	if w1.SessionID == "" {
		t.Fatalf("welcome had empty session id")
	}
	var buf1 bytes.Buffer
	readUntilReplayDone(t, c1, &buf1)
	if err := wire.WriteFrame(c1, wire.FrameData, []byte("echo HIVE_REPLAY_TEST_$((20+22))\n")); err != nil {
		t.Fatalf("write data: %v", err)
	}
	drainFor(c1, &buf1, 800*time.Millisecond)
	if !strings.Contains(buf1.String(), "HIVE_REPLAY_TEST_42") {
		t.Fatalf("expected marker on first attach: %q", buf1.String())
	}
	_ = c1.Close()

	// Second attach: the replay should contain the marker we just saw.
	c2 := dial()
	w2 := handshake(t, c2)
	if w2.SessionID != w1.SessionID {
		t.Errorf("session id changed across reattach: %s → %s", w1.SessionID, w2.SessionID)
	}
	var replay bytes.Buffer
	readUntilReplayDone(t, c2, &replay)
	if !bytes.Contains(replay.Bytes(), []byte("HIVE_REPLAY_TEST_42")) {
		t.Errorf("replay missing prior output; got %q", replay.String())
	}
	_ = c2.Close()
}

func TestDaemonResize(t *testing.T) {
	skipOnWindows(t)
	d, _ := startTestDaemon(t)

	conn, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = handshake(t, conn)
	var buf bytes.Buffer
	readUntilReplayDone(t, conn, &buf)

	if err := wire.WriteJSON(conn, wire.FrameResize, wire.Resize{Cols: 137, Rows: 41}); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := wire.WriteFrame(conn, wire.FrameData, []byte("stty size\n")); err != nil {
		t.Fatalf("write stty: %v", err)
	}
	drainFor(conn, &buf, 1500*time.Millisecond)
	if !strings.Contains(buf.String(), "41 137") {
		t.Errorf("expected 41 137; got %q", buf.String())
	}
}

func TestDaemonRefusesProtocolMismatch(t *testing.T) {
	skipOnWindows(t)
	d, _ := startTestDaemon(t)

	conn, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{Version: 999, Client: "bad"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	ft, _, err := wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ft != wire.FrameError {
		t.Errorf("expected ERROR for bad version, got %s", ft)
	}
	// Connection should be closed shortly after.
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, _, err := wire.ReadFrame(conn); err == nil {
		t.Errorf("expected connection close after version mismatch")
	}
}
