package wire

import (
	"bytes"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// handshakeServer answers one HELLO on the server side of a pipe with
// the given reply frame, then returns the HELLO it saw.
func handshakeServer(t *testing.T, srv net.Conn, replyType FrameType, reply any) *Hello {
	t.Helper()
	var hello Hello
	if ft, err := ReadJSON(srv, &hello); err != nil || ft != FrameHello {
		t.Errorf("server: expected HELLO, got %s err=%v", ft, err)
		return nil
	}
	if err := WriteJSON(srv, replyType, reply); err != nil {
		t.Errorf("server: write reply: %v", err)
	}
	return &hello
}

func TestHandshake_Welcome(t *testing.T) {
	cli, srv := net.Pipe()
	var gotHello *Hello
	done := make(chan struct{})
	go func() {
		defer close(done)
		gotHello = handshakeServer(t, srv, FrameWelcome, Welcome{
			Version: PROTOCOL_VERSION, Mode: ModeAttach, SessionID: "s1", Cols: 80, Rows: 24,
		})
	}()

	c, err := Handshake(cli, Hello{Mode: ModeAttach, SessionID: "s1", Client: "test/0"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	defer c.Close()
	<-done

	// Version is filled when zero — every production client relies on this.
	if gotHello == nil || gotHello.Version != PROTOCOL_VERSION {
		t.Errorf("sent hello version = %+v, want %d", gotHello, PROTOCOL_VERSION)
	}
	w := c.Welcome()
	if w.SessionID != "s1" || w.Cols != 80 || w.Rows != 24 {
		t.Errorf("welcome = %+v", w)
	}
}

func TestHandshake_PreservesNonZeroVersion(t *testing.T) {
	// The e2e suite probes version rejection by sending Version: 999;
	// Handshake must not overwrite it.
	cli, srv := net.Pipe()
	done := make(chan struct{})
	var gotHello *Hello
	go func() {
		defer close(done)
		gotHello = handshakeServer(t, srv, FrameError, Error{Code: "version_mismatch", Message: "go away"})
	}()

	_, err := Handshake(cli, Hello{Version: 999, Mode: ModeControl})
	<-done
	if err == nil {
		t.Fatal("expected error on refused handshake")
	}
	if !strings.Contains(err.Error(), "go away") {
		t.Errorf("refusal error should carry daemon message, got: %v", err)
	}
	if gotHello == nil || gotHello.Version != 999 {
		t.Errorf("hello version = %+v, want 999", gotHello)
	}
}

func TestHandshake_RefusalWithoutMessage(t *testing.T) {
	cli, srv := net.Pipe()
	go handshakeServer(t, srv, FrameError, Error{})
	if _, err := Handshake(cli, Hello{Mode: ModeControl}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHandshake_UnexpectedFrame(t *testing.T) {
	cli, srv := net.Pipe()
	go handshakeServer(t, srv, FrameData, nil)
	_, err := Handshake(cli, Hello{Mode: ModeControl})
	if err == nil || !strings.Contains(err.Error(), "expected WELCOME") {
		t.Fatalf("err = %v", err)
	}
	// Conn must be closed on failure: a subsequent server read fails.
	if err := WriteFrame(srv, FrameData, []byte("x")); err == nil {
		var b [1]byte
		if _, rerr := srv.Read(b[:]); rerr == nil {
			t.Error("conn still open after failed handshake")
		}
	}
}

func TestHandshake_TimesOutOnMuteServer(t *testing.T) {
	old := handshakeTimeout
	handshakeTimeout = 50 * time.Millisecond
	defer func() { handshakeTimeout = old }()

	cli, srv := net.Pipe()
	defer srv.Close()
	// Server reads HELLO but never answers WELCOME.
	go func() {
		var hello Hello
		_, _ = ReadJSON(srv, &hello)
	}()
	if _, err := Handshake(cli, Hello{Mode: ModeControl}); err == nil {
		t.Fatal("expected timeout error on mute server")
	}
}

// TestClient_ConcurrentWritesDoNotInterleave pins the reason the write
// mutex exists: header and payload are two Writes, and two goroutines
// writing the same conn unserialized corrupt the stream.
func TestClient_ConcurrentWritesDoNotInterleave(t *testing.T) {
	cli, srv := net.Pipe()
	go handshakeServer(t, srv, FrameWelcome, Welcome{Version: PROTOCOL_VERSION})
	c, err := Handshake(cli, Hello{Mode: ModeAttach, SessionID: "x"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	defer c.Close()

	const writers, frames = 8, 50
	payload := bytes.Repeat([]byte("ab"), 100)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for range frames {
				if err := c.WriteFrame(FrameData, payload); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		})
	}
	go func() { wg.Wait(); _ = srv.Close() }()

	got := 0
	for {
		ft, p, err := ReadFrame(srv)
		if err != nil {
			break
		}
		if ft != FrameData || !bytes.Equal(p, payload) {
			t.Fatalf("corrupt frame: type=%s len=%d", ft, len(p))
		}
		got++
	}
	if got != writers*frames {
		t.Errorf("frames received = %d, want %d", got, writers*frames)
	}
}

func TestEventNameTables(t *testing.T) {
	cases := []struct {
		fn    func(FrameType) (string, bool)
		t     FrameType
		want  string
		known bool
	}{
		{ControlEventName, FrameSessions, "session:list", true},
		{ControlEventName, FrameSessionEvent, "session:event", true},
		{ControlEventName, FrameProjects, "project:list", true},
		{ControlEventName, FrameProjectEvent, "project:event", true},
		{ControlEventName, FrameError, "control:error", true},
		{ControlEventName, FrameData, "", false},
		{AttachEventName, FrameData, "pty:data", true},
		{AttachEventName, FrameEvent, "pty:event", true},
		{AttachEventName, FrameError, "pty:error", true},
		{AttachEventName, FrameSessions, "", false},
	}
	for _, tc := range cases {
		got, ok := tc.fn(tc.t)
		if got != tc.want || ok != tc.known {
			t.Errorf("event name for %s: got (%q,%v), want (%q,%v)", tc.t, got, ok, tc.want, tc.known)
		}
	}
}
