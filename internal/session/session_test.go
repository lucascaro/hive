package session

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("session E2E tests require POSIX shell")
	}
}

type bufSink struct {
	mu  *bufSinkMu
}
type bufSinkMu struct {
	buf bytes.Buffer
}

func (b *bufSinkMu) Write(p []byte) (int, error) {
	return b.buf.Write(p)
}

func (b *bufSinkMu) String() string { return b.buf.String() }

// drainUntil polls fn until it returns true or the deadline expires.
func drainUntil(fn func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fn()
}

func TestSessionEchoAndPersistsState(t *testing.T) {
	skipOnWindows(t)
	sess, err := Start(Options{Shell: "/bin/bash", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	sink := &bufSinkMu{}
	_, unsub := sess.SubscribeAtomicSnapshot(sink)
	defer unsub()

	// Send a command, expect to see its output in the sink.
	if _, err := sess.Write([]byte("echo HIVE_PROBE_$((1+1))\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !drainUntil(func() bool { return strings.Contains(sink.String(), "HIVE_PROBE_2") }, 2*time.Second) {
		t.Fatalf("expected HIVE_PROBE_2 in output, got %q", sink.String())
	}

	// Set state, then re-subscribe with a fresh sink: the scrollback
	// snapshot should include the prior output.
	if _, err := sess.Write([]byte("export HIVE_FOO=marker_42\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Give it a moment to round-trip.
	time.Sleep(200 * time.Millisecond)

	sink2 := &bufSinkMu{}
	replay, unsub2 := sess.SubscribeAtomicSnapshot(sink2)
	defer unsub2()

	if !bytes.Contains(replay, []byte("HIVE_PROBE_2")) {
		t.Errorf("scrollback replay missing prior output; got %q", replay)
	}

	// Live stream should also work after re-subscribe.
	if _, err := sess.Write([]byte("echo done_$HIVE_FOO\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !drainUntil(func() bool { return strings.Contains(sink2.String(), "done_marker_42") }, 2*time.Second) {
		t.Fatalf("expected done_marker_42 after re-subscribe; got %q", sink2.String())
	}
}

func TestSessionResize(t *testing.T) {
	skipOnWindows(t)
	sess, err := Start(Options{Shell: "/bin/bash", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()

	sink := &bufSinkMu{}
	_, unsub := sess.SubscribeAtomicSnapshot(sink)
	defer unsub()

	if err := sess.Resize(132, 50); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Wait briefly for SIGWINCH to be delivered before asking stty.
	time.Sleep(100 * time.Millisecond)
	if _, err := sess.Write([]byte("stty size\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !drainUntil(func() bool { return strings.Contains(sink.String(), "50 132") }, 2*time.Second) {
		t.Fatalf("expected 50 132 from stty; got %q", sink.String())
	}
}

