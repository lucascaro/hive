package session

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("session E2E tests require POSIX shell")
	}
}

type bufSinkMu struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bufSinkMu) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bufSinkMu) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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

func TestCmdExeEscape(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"single plain word", []string{"claude"}, `"claude"`},
		{"flag with value", []string{"claude", "--model", "opus"}, `"claude" "--model" "opus"`},
		{"arg with space", []string{"claude", "--name", "claude opus"}, `"claude" "--name" "claude opus"`},
		{"empty string preserved", []string{"foo", ""}, `"foo" ""`},
		{"cmd metacharacters inside quotes", []string{"echo", "a&b|c^d>e<f%g"}, `"echo" "a&b|c^d>e<f%g"`},
		{"embedded double quote", []string{"echo", `she said "hi"`}, `"echo" "she said \"hi\""`},
		{"trailing backslashes get doubled before closing quote", []string{"x", `c:\path\`}, `"x" "c:\path\\"`},
		{"backslash before quote", []string{"x", `a\"b`}, `"x" "a\\\"b"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cmdExeEscape(c.in)
			if got != c.want {
				t.Fatalf("cmdExeEscape(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestStartSpawnsCmdOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only spawn path")
	}
	sess, err := Start(Options{Cmd: []string{"cmd.exe", "/C", "echo hivetest"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()
	sink := &bufSinkMu{}
	_, unsub := sess.SubscribeAtomicSnapshot(sink)
	defer unsub()
	if !drainUntil(func() bool { return strings.Contains(sink.String(), "hivetest") }, 5*time.Second) {
		t.Fatalf("expected hivetest in output, got %q", sink.String())
	}
}

func TestStartSpawnsCmdOnUnix(t *testing.T) {
	skipOnWindows(t)
	sess, err := Start(Options{Shell: "/bin/bash", Cmd: []string{"echo", "hivetest"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()
	sink := &bufSinkMu{}
	_, unsub := sess.SubscribeAtomicSnapshot(sink)
	defer unsub()
	if !drainUntil(func() bool { return strings.Contains(sink.String(), "hivetest") }, 5*time.Second) {
		t.Fatalf("expected hivetest in output, got %q", sink.String())
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

