// The e2e build (-tags=e2e) has its own TestMain in e2e_test.go; like
// the Claude probe this one is opt-in and lives in the default build.
//go:build !e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// startPiTestDaemon is startHookTestDaemon plus the state dir, which
// the Pi tier needs: the extension is written under it and the adapter
// only passes `-e` when that file exists.
func startPiTestDaemon(t *testing.T) (*daemon.Daemon, string) {
	t.Helper()
	tmp, err := os.MkdirTemp("/tmp", "hived-pi")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	stateDir := filepath.Join(tmp, "state")
	if err := agent.EnsurePiExtension(stateDir); err != nil {
		t.Fatalf("EnsurePiExtension: %v", err)
	}
	d, err := daemon.New(daemon.Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   stateDir,
		BootstrapSession: session.Options{
			Shell: "/bin/bash", Cols: 80, Rows: 24,
		},
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = d.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = d.Close()
		<-runDone
	})
	return d, stateDir
}

// TestPiProbeReportsThroughTheExtension is the live check for the
// extension tier, the one thing no fixture can prove: a REAL pi,
// launched by the daemon with the embedded extension, reports its own
// state over the event socket. Opt-in — it costs one API call:
//
//	HIVE_PROBE_PI=1 go test ./cmd/hived/ -run TestPiProbe -v
func TestPiProbeReportsThroughTheExtension(t *testing.T) {
	if os.Getenv("HIVE_PROBE_PI") != "1" {
		t.Skip("set HIVE_PROBE_PI=1 to run the real-pi probe")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skip("pi not on PATH")
	}
	// Preflight: the daemon launches agents through a login shell, so
	// the `pi` that matters is the one that shell resolves — with the
	// node IT puts on PATH. A machine whose login shell still points at
	// an old node runs a pi that dies in its own bundle before the
	// extension ever loads, and the probe would report that as a bare
	// timeout on the extension tier. Say which it was.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	if out, err := exec.Command(shell, "-l", "-i", "-c", "pi --version").CombinedOutput(); err != nil {
		t.Skipf("login shell cannot run pi (%v): %s", err, out)
	}

	d, _ := startPiTestDaemon(t)
	cwd := t.TempDir()

	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
		Agent: "pi", Cwd: cwd, Cols: 120, Rows: 40,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := e.ID
	t.Cleanup(func() { _ = d.Registry().Kill(id, true) })

	wait := func(within time.Duration, cond func(wire.SessionInfo) bool, what string) wire.SessionInfo {
		t.Helper()
		deadline := time.Now().Add(within)
		var info wire.SessionInfo
		for {
			var ok bool
			info, ok = findSessionByID(d, id)
			if ok && cond(info) {
				return info
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s; last info = %+v", what, info)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	wait(20*time.Second, func(i wire.SessionInfo) bool { return i.Alive }, "session alive")
	sess := d.Registry().Get(id).Session()
	if sess == nil {
		t.Fatal("no live session")
	}
	sink := &captureSink{}
	unsub, err := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error {
		_, err := sink.Write(replay)
		return err
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	t.Cleanup(func() {
		if dir := os.Getenv("HIVE_PROBE_DUMP"); dir != "" {
			sink.mu.Lock()
			_ = os.WriteFile(dir+"/pi-probe-screen.bin", sink.buf.Bytes(), 0o600)
			sink.mu.Unlock()
		}
	})

	// session_start is a ping: it promotes the tier without moving state.
	wait(60*time.Second, func(i wire.SessionInfo) bool {
		return i.StateSource == wire.StateSourceExtension
	}, "extension tier (session_start ping)")

	time.Sleep(2 * time.Second)
	if _, err := sess.Write([]byte("reply with the single word pong\r")); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	info := wait(30*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateWorking && i.StateSource == wire.StateSourceExtension
	}, "working after prompt")
	if !strings.Contains(info.LastPrompt, "pong") {
		t.Errorf("LastPrompt = %q, want the typed prompt", info.LastPrompt)
	}

	// The reply lands the session back on idle through agent_settled,
	// still on the extension tier — not on the heuristic quiet tick.
	info = wait(120*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateIdle && i.StateSource == wire.StateSourceExtension
	}, "idle after the reply")
	if info.LastSummary == "" {
		t.Errorf("LastSummary is empty after a completed turn")
	}
}
