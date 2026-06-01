package client

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// fakeTerm is an in-memory Terminal: stdin is a pipe we write to, stdout
// is a buffer we inspect, size is fixed, and resize never fires.
type fakeTerm struct {
	in     io.Reader
	out    *syncBuf
	resize chan struct{}
}

func (f *fakeTerm) Stdin() io.Reader              { return f.in }
func (f *fakeTerm) Stdout() io.Writer             { return f.out }
func (f *fakeTerm) Size() (int, int, error)       { return 80, 24, nil }
func (f *fakeTerm) ResizeEvents() <-chan struct{} { return f.resize }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// serverEnd runs a minimal fake daemon attach side on one end of a
// socketpair: it sends WELCOME then DATA("hi"), and records bytes the
// client forwards.
func serverEnd(t *testing.T, conn net.Conn, gotInput *syncBuf, done chan<- struct{}) {
	defer close(done)
	if ft, _, err := wire.ReadFrame(conn); err != nil || ft != wire.FrameHello {
		t.Errorf("server: want HELLO, got %s err=%v", ft, err)
		return
	}
	_ = wire.WriteJSON(conn, wire.FrameWelcome, wire.Welcome{Mode: wire.ModeAttach, Cols: 80, Rows: 24})
	_ = wire.WriteFrame(conn, wire.FrameData, []byte("hi"))
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return
		}
		if ft == wire.FrameData {
			gotInput.Write(payload)
		}
	}
}

func TestAttach_StreamsBothWaysAndDetaches(t *testing.T) {
	srv, cli := net.Pipe()
	gotInput := &syncBuf{}
	srvDone := make(chan struct{})
	go serverEnd(t, srv, gotInput, srvDone)

	// Client stdin: type "ok" then Ctrl-A d to detach.
	stdin := bytes.NewReader([]byte{'o', 'k', 0x01, 'd'})
	out := &syncBuf{}
	term := &fakeTerm{in: stdin, out: out, resize: make(chan struct{})}

	errc := make(chan error, 1)
	go func() { errc <- Attach(cli, "sess1", term, AttachOptions{RequestReplay: false}) }()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Attach returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Attach did not return after detach")
	}

	if !bytes.Contains([]byte(out.String()), []byte("hi")) {
		t.Errorf("stdout missing server data %q", out.String())
	}
	srv.Close()
	<-srvDone
	if got := gotInput.String(); got != "ok" {
		t.Errorf("server got input %q, want %q (Ctrl-A d must not be forwarded)", got, "ok")
	}
}
