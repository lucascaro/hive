package testclient

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// shortTempDir mirrors the helper used by daemon tests: keeps the
// unix socket path under macOS's 104-byte sun_path cap.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tcl")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// fakeDaemon is a tiny in-test stand-in for hived used to exercise
// the testclient frame de-mux without spawning the real binary.
type fakeDaemon struct {
	t      *testing.T
	ln     net.Listener
	conn   net.Conn
	accept chan struct{}
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	tmp := shortTempDir(t)
	sock := filepath.Join(tmp, "fake.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Setenv("HIVE_SOCKET", sock)
	t.Setenv("HIVE_STATE_DIR", filepath.Join(tmp, "state"))
	fd := &fakeDaemon{t: t, ln: ln, accept: make(chan struct{})}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		fd.conn = c
		close(fd.accept)
	}()
	return fd
}

func (f *fakeDaemon) sockPath() string {
	return f.ln.Addr().String()
}

func (f *fakeDaemon) waitAccept(t *testing.T) {
	t.Helper()
	select {
	case <-f.accept:
	case <-time.After(2 * time.Second):
		t.Fatalf("fakeDaemon: no accept")
	}
}

func TestClient_Handshake_ReturnsWelcome(t *testing.T) {
	fd := newFakeDaemon(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	// Server side: read HELLO, write WELCOME.
	go func() {
		var hello wire.Hello
		if _, err := wire.ReadJSON(fd.conn, &hello); err != nil {
			t.Errorf("server read hello: %v", err)
			return
		}
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{
			Version: wire.PROTOCOL_VERSION,
			Mode:    wire.ModeControl,
		})
	}()

	w, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if w.Version != wire.PROTOCOL_VERSION {
		t.Errorf("welcome version: got %d, want %d", w.Version, wire.PROTOCOL_VERSION)
	}
}

func TestClient_Handshake_ReturnsErrorOnRefusal(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameError, wire.Error{
			Code: "version_mismatch", Message: "go away",
		})
	}()

	if _, err := cli.Handshake(wire.Hello{Version: 999, Mode: wire.ModeControl}); err == nil {
		t.Fatal("expected error on refused handshake")
	}
}

func TestClient_WaitForData_FindsMarker(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	// Server: read HELLO, send WELCOME, then dribble bytes containing a
	// marker across multiple DATA frames.
	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		_ = wire.WriteFrame(fd.conn, wire.FrameData, []byte("hello "))
		_ = wire.WriteFrame(fd.conn, wire.FrameData, []byte("MARK_"))
		_ = wire.WriteFrame(fd.conn, wire.FrameData, []byte("123 world"))
	}()

	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeAttach, SessionID: "x"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	got, err := cli.WaitForData([]byte("MARK_123"), time.Second)
	if err != nil {
		t.Fatalf("WaitForData: %v", err)
	}
	if string(got) != "hello MARK_123 world" {
		t.Errorf("buf: got %q", got)
	}
}

func TestClient_AwaitReplayBoundary_DelimitsCorrectly(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		// Pre-replay noise that AwaitReplayBoundary should discard.
		_ = wire.WriteFrame(fd.conn, wire.FrameData, []byte("noise"))
		// Begin/Done envelope around the replay payload.
		_ = wire.WriteJSON(fd.conn, wire.FrameEvent, wire.Event{Kind: wire.EventScrollbackReplayBegin})
		_ = wire.WriteFrame(fd.conn, wire.FrameData, []byte("REPLAY_BYTES"))
		_ = wire.WriteJSON(fd.conn, wire.FrameEvent, wire.Event{Kind: wire.EventScrollbackReplayDone})
	}()

	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeAttach, SessionID: "x"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	replay, err := cli.AwaitReplayBoundary(2 * time.Second)
	if err != nil {
		t.Fatalf("AwaitReplayBoundary: %v", err)
	}
	if string(replay) != "REPLAY_BYTES" {
		t.Errorf("replay payload: got %q, want %q", replay, "REPLAY_BYTES")
	}
}

func TestClient_AwaitSessionEvent_FiltersByKind(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		_ = wire.WriteJSON(fd.conn, wire.FrameSessionEvent, wire.SessionEvent{
			Kind:    wire.SessionEventUpdated,
			Session: wire.SessionInfo{ID: "s1", Name: "renamed"},
		})
		_ = wire.WriteJSON(fd.conn, wire.FrameSessionEvent, wire.SessionEvent{
			Kind:    wire.SessionEventRemoved,
			Session: wire.SessionInfo{ID: "s1"},
		})
	}()

	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	ev, err := cli.AwaitSessionEvent(wire.SessionEventRemoved, 2*time.Second)
	if err != nil {
		t.Fatalf("AwaitSessionEvent: %v", err)
	}
	if ev.Kind != wire.SessionEventRemoved || ev.Session.ID != "s1" {
		t.Errorf("ev: %+v", ev)
	}
}

func TestRequireIsolation(t *testing.T) {
	cases := []struct {
		name    string
		sock    string
		state   string
		wantErr bool
	}{
		{name: "both unset", wantErr: true},
		{name: "only sock", sock: "/tmp/x/h.sock", wantErr: true},
		{name: "outside tmp", sock: "/home/u/h.sock", state: "/home/u/state", wantErr: true},
		{name: "both tmp", sock: "/tmp/x/h.sock", state: "/tmp/x/state", wantErr: false},
		{name: "private tmp (macOS)", sock: "/private/tmp/x/h.sock", state: "/private/tmp/x/state", wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HIVE_SOCKET", tc.sock)
			t.Setenv("HIVE_STATE_DIR", tc.state)
			err := RequireIsolation()
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// Compile-time check: SessionInfo JSON shape doesn't drift in a way
// that breaks the testclient's downstream consumers. Mirrors the
// project-memory note about snake_case payloads.
func TestSessionInfo_JSONShape(t *testing.T) {
	info := wire.SessionInfo{ID: "i", Name: "n", ProjectID: "p", WorktreePath: "/wt"}
	b, _ := json.Marshal(info)
	s := string(b)
	for _, want := range []string{`"project_id"`, `"worktree_path"`} {
		if !contains(s, want) {
			t.Errorf("SessionInfo JSON missing %s: %s", want, s)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
