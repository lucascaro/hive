package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// shortTempDir keeps socket paths well clear of sun_path's 104-byte
// limit on macOS, which t.TempDir() gets uncomfortably close to.
// Mirrors the helper of the same name in internal/daemon.
//
// The /tmp base is unix-only — there is no such directory on Windows,
// and sun_path is not a constraint there either, so fall back to the
// default temp location.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = ""
	}
	dir, err := os.MkdirTemp(base, "hg")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// socketDead is the restart path's liveness authority, replacing the
// signal-based probe that reports a zombie hived as alive forever.
// These two tests pin both answers.

func TestSocketDead_LiveListener(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "s")
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
	sock := filepath.Join(shortTempDir(t), "s")
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
	if !socketDead(filepath.Join(shortTempDir(t), "nope"), time.Second) {
		t.Fatal("socketDead reported alive for a nonexistent socket path")
	}
}

// TestRestartDaemon_FailurePreservesConns is the regression test for
// the ordering bug: RestartDaemon used to tear down the control and
// attach conns before it knew whether the daemon would actually die.
// On the "hived survived" path that left the user in a dead window —
// there is no recovery route, since ConnectControl only runs from the
// frontend's boot path. The error path must leave everything wired up.
func TestRestartDaemon_FailurePreservesConns(t *testing.T) {
	// NewApp calls agent.SetCustomDir(registry.StateDir()). Without
	// this the test reads the developer's real Hive state directory
	// and leaves the package-level customDir pointing at it for the
	// rest of the binary.
	isolateState(t)
	sock := filepath.Join(shortTempDir(t), "s")
	// A daemon that ignores FrameShutdown and never dies.
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	// Hold the accepted conns in a slice, not in a discarded local: a
	// net.Conn that becomes unreachable is closed by its finalizer
	// whenever the GC next runs, which closed the client end mid-test
	// and failed the "still writable" assertion below on Windows CI.
	var mu sync.Mutex
	var accepted []net.Conn
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range accepted {
			_ = c.Close()
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			accepted = append(accepted, c) // hold it open, read nothing, exit never
			mu.Unlock()
		}
	}()
	t.Setenv("HIVE_SOCKET", sock)

	control, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	defer control.Close()
	attach, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial attach: %v", err)
	}
	defer attach.Close()

	app := NewApp("")
	app.control = wire.NewClient(control)
	app.attaches["sess-1"] = wire.NewClient(attach)

	if err := app.RestartDaemon(); err == nil {
		t.Fatal("RestartDaemon should fail while the daemon is still answering")
	}

	if app.control == nil {
		t.Error("control conn was dropped on the failure path")
	}
	if len(app.attaches) != 1 {
		t.Errorf("attach conns were dropped on the failure path: %d left", len(app.attaches))
	}
	// Still usable, not a closed fd.
	if _, err := control.Write([]byte{}); err != nil {
		t.Errorf("control conn is no longer writable: %v", err)
	}
}
