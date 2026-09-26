package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// newPipeSink builds a frameSink over a zero-buffer net.Pipe, so "the
// client stopped reading" is simply "the test does not read". The
// limits are captured at construction, so they are restored at once.
func newPipeSink(t *testing.T, timeout time.Duration, limit int) (*frameSink, net.Conn) {
	t.Helper()
	oldT, oldL := clientWriteTimeout.Load(), attachBacklogLimit.Load()
	clientWriteTimeout.Store(int64(timeout))
	attachBacklogLimit.Store(int64(limit))
	server, client := net.Pipe()
	f := newFrameSink(server)
	clientWriteTimeout.Store(oldT)
	attachBacklogLimit.Store(oldL)
	t.Cleanup(func() {
		f.stop()
		_ = client.Close()
	})
	return f, client
}

func waitDone(t *testing.T, f *frameSink, within time.Duration) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(within):
		t.Fatalf("frame sink writer did not exit within %v", within)
	}
}

// readErrWithin reads until the conn errors, failing if that takes
// longer than within. It is how a test observes "the daemon hung up".
func readErrWithin(t *testing.T, c net.Conn, within time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(within))
	buf := make([]byte, 64<<10)
	for {
		if _, err := c.Read(buf); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				return
			}
			t.Fatalf("expected the sink to hang up, got %v", err)
		}
	}
}

func readFrame(t *testing.T, c net.Conn) (wire.FrameType, []byte) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	ft, p, err := wire.ReadFrame(c)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	return ft, p
}

func readEventKind(t *testing.T, c net.Conn) string {
	t.Helper()
	ft, p := readFrame(t, c)
	if ft != wire.FrameEvent {
		t.Fatalf("frame = %s, want EVENT", ft)
	}
	var ev wire.Event
	if err := json.Unmarshal(p, &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	return string(ev.Kind)
}

func readData(t *testing.T, c net.Conn) []byte {
	t.Helper()
	ft, p := readFrame(t, c)
	if ft != wire.FrameData {
		t.Fatalf("frame = %s, want DATA", ft)
	}
	return p
}

// The session calls Write while holding Session.mu. A client that has
// stopped reading must not hold that call: past the backlog limit the
// sink refuses (so the session drops it) and hangs up.
func TestFrameSinkWriteNeverBlocksOnStalledReader(t *testing.T) {
	f, client := newPipeSink(t, time.Hour, 16<<10)

	chunk := bytes.Repeat([]byte("x"), 4096)
	errc := make(chan error, 1)
	go func() {
		for i := 0; i < 1000; i++ {
			if _, err := f.Write(chunk); err != nil {
				errc <- err
				return
			}
		}
		errc <- nil
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, errSinkBacklog) {
			t.Fatalf("Write err = %v, want errSinkBacklog", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked on a client that stopped reading")
	}
	waitDone(t, f, 5*time.Second)
	readErrWithin(t, client, 5*time.Second)
	if _, err := f.Write(chunk); err == nil {
		t.Fatal("Write after hang-up succeeded; the session would keep a dead sink")
	}
}

// Within the backlog, a client that takes no frame for the write
// timeout is hung up on too: a quiet session must not keep a dead
// client (and its writer goroutine) forever.
func TestFrameSinkWriteDeadlineDisconnects(t *testing.T) {
	f, client := newPipeSink(t, 50*time.Millisecond, 8<<20)
	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitDone(t, f, 5*time.Second)
	if _, err := f.Write([]byte("again")); err == nil {
		t.Fatal("Write after the deadline hang-up succeeded")
	}
	readErrWithin(t, client, 5*time.Second)
}

// The PTY readLoop reuses its read buffer the moment deliver returns,
// while the frame is still queued. The sink must own a copy.
func TestFrameSinkCopiesInput(t *testing.T) {
	f, client := newPipeSink(t, time.Hour, 8<<20)
	buf := []byte("aaaa")
	if _, err := f.Write(buf); err != nil {
		t.Fatal(err)
	}
	copy(buf, "bbbb")
	if _, err := f.Write(buf); err != nil {
		t.Fatal(err)
	}
	if got := string(readData(t, client)); got != "aaaa" {
		t.Fatalf("first frame = %q, want aaaa (buffer was not copied)", got)
	}
	if got := string(readData(t, client)); got != "bbbb" {
		t.Fatalf("second frame = %q, want bbbb", got)
	}
}

// Replay and live output share one FIFO, so the order the session
// queued them in (under Session.mu) is the order they reach the wire.
func TestFrameSinkPreservesReplayThenLiveOrder(t *testing.T) {
	f, client := newPipeSink(t, time.Hour, 8<<20)
	replay := bytes.Repeat([]byte("r"), 40<<10)
	if _, err := f.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	if err := f.writeReplay(replay, 16<<10); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("B")); err != nil {
		t.Fatal(err)
	}

	if got := string(readData(t, client)); got != "A" {
		t.Fatalf("frame 1 = %q, want A", got)
	}
	if k := readEventKind(t, client); k != string(wire.EventScrollbackReplayBegin) {
		t.Fatalf("frame 2 kind = %s, want replay begin", k)
	}
	var got []byte
	for len(got) < len(replay) {
		got = append(got, readData(t, client)...)
	}
	if !bytes.Equal(got, replay) {
		t.Fatalf("replay bytes differ: got %d bytes", len(got))
	}
	if k := readEventKind(t, client); k != string(wire.EventScrollbackReplayDone) {
		t.Fatalf("after replay kind = %s, want replay done", k)
	}
	if got := string(readData(t, client)); got != "B" {
		t.Fatalf("last frame = %q, want B", got)
	}
}

// A replay can be as large as the session's scrollback ring, far past
// the live backlog limit. It must always be delivered, not treated as
// a client falling behind.
func TestFrameSinkReplayLargerThanLimitIsAccepted(t *testing.T) {
	f, client := newPipeSink(t, time.Hour, 1<<10)
	replay := bytes.Repeat([]byte("r"), 64<<10)
	if err := f.writeReplay(replay, 16<<10); err != nil {
		t.Fatalf("writeReplay: %v", err)
	}
	if k := readEventKind(t, client); k != string(wire.EventScrollbackReplayBegin) {
		t.Fatalf("kind = %s, want replay begin", k)
	}
	var got []byte
	for len(got) < len(replay) {
		got = append(got, readData(t, client)...)
	}
	if k := readEventKind(t, client); k != string(wire.EventScrollbackReplayDone) {
		t.Fatalf("kind = %s, want replay done", k)
	}
}

// The replay's allowance is temporary. Once the replay is on the wire
// the limit is back to its base, so a large replay cannot permanently
// raise how far behind a client may fall.
func TestFrameSinkReplayBudgetDrains(t *testing.T) {
	const limit = 1 << 10
	f, client := newPipeSink(t, time.Hour, limit)
	if err := f.writeReplay(bytes.Repeat([]byte("r"), 64<<10), 16<<10); err != nil {
		t.Fatal(err)
	}
	readEventKind(t, client)
	for {
		ft, _ := readFrame(t, client)
		if ft == wire.FrameEvent {
			break
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		owed := f.replayOwed
		f.mu.Unlock()
		if owed == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("replayOwed = %d after the replay was read, want 0", owed)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Stop reading. The first write is taken by the writer and blocks on
	// the pipe; the rest queue until the base limit is crossed.
	accepted := 0
	chunk := bytes.Repeat([]byte("x"), 256)
	for accepted < 64<<10 {
		if _, err := f.Write(chunk); err != nil {
			break
		}
		accepted += len(chunk)
	}
	if accepted > 4*limit {
		t.Fatalf("accepted %d bytes of live backlog after the replay drained; want about %d", accepted, limit)
	}
}

// The session closes its sinks when the PTY ends. The output queued
// just before that (often an agent's last lines) must still arrive.
func TestFrameSinkCloseFlushesQueuedOutput(t *testing.T) {
	f, client := newPipeSink(t, time.Hour, 8<<20)
	want := []string{"one", "two", "three", "four", "five"}
	for _, s := range want {
		if _, err := f.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, s := range want {
		if got := string(readData(t, client)); got != s {
			t.Fatalf("frame = %q, want %q", got, s)
		}
	}
	readErrWithin(t, client, 5*time.Second)
	waitDone(t, f, 5*time.Second)
}

// An idle writer is parked on its wake channel, not in a conn write,
// so closing the conn alone would not free it. stop must.
func TestFrameSinkStopWakesIdleWriter(t *testing.T) {
	f, _ := newPipeSink(t, time.Hour, 8<<20)
	f.stop()
	waitDone(t, f, 5*time.Second)
}
