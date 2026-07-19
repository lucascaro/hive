package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// socketDead is the restart path's liveness authority, replacing the
// signal-based probe that reports a zombie hived as alive forever.
// These two tests pin both answers.

func TestSocketDead_LiveListener(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	start := time.Now()
	if socketDead(sock, 300*time.Millisecond) {
		t.Fatal("socketDead reported dead while a listener is accepting")
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("gave up after %v, want at least the full 300ms budget", elapsed)
	}
}

func TestSocketDead_AfterClose(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close()

	if !socketDead(sock, time.Second) {
		t.Fatal("socketDead reported alive after the listener closed")
	}
}

// A socket path that never existed must read as dead, not as an
// error the caller has to interpret — RestartDaemon treats "cannot
// dial" as "nothing to reconnect to", which is the whole point.
func TestSocketDead_MissingPath(t *testing.T) {
	if !socketDead(filepath.Join(t.TempDir(), "nope"), time.Second) {
		t.Fatal("socketDead reported alive for a nonexistent socket path")
	}
}
