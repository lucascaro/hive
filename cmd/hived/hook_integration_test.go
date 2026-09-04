package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// startHookTestDaemon brings up a real daemon on an isolated temp
// socket + state dir (never the user's real state — see project
// memory on e2e isolation) with one bootstrap session.
func startHookTestDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook integration test requires a POSIX shell")
	}
	tmp, err := os.MkdirTemp("/tmp", "hived-hook")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	d, err := daemon.New(daemon.Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash", Cols: 80, Rows: 24,
		},
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = d.Close()
	})
	return d
}

// dialControlAndSubscribe opens a control connection, drains the
// initial PROJECTS + SESSIONS snapshot, and returns a channel of every
// subsequent SESSION_EVENT.
func dialControlAndSubscribe(t *testing.T, d *daemon.Daemon) (net.Conn, chan wire.SessionEvent) {
	t.Helper()
	conn, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0", Mode: wire.ModeControl,
	}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var w wire.Welcome
	if ft, err := wire.ReadJSON(conn, &w); err != nil || ft != wire.FrameWelcome {
		t.Fatalf("welcome: ft=%s err=%v", ft, err)
	}
	events := make(chan wire.SessionEvent, 256)
	go func() {
		for {
			ft, payload, err := wire.ReadFrame(conn)
			if err != nil {
				close(events)
				return
			}
			switch ft {
			case wire.FrameProjects, wire.FrameSessions:
				// initial snapshot; ignore
			case wire.FrameSessionEvent:
				var se wire.SessionEvent
				if err := json.Unmarshal(payload, &se); err == nil {
					events <- se
				}
			}
		}
	}()
	return conn, events
}

func findSessionByID(d *daemon.Daemon, id string) (wire.SessionInfo, bool) {
	for _, s := range d.Registry().List() {
		if s.ID == id {
			return s, true
		}
	}
	return wire.SessionInfo{}, false
}

// runHookFixture runs the hook code path (mapHookPayload + send) for
// one fixture file against a live daemon, with HIVE_SESSION_ID/
// HIVE_SOCKET set to point at it — exactly what Claude invoking
// `hived hook` would do.
func runHookFixture(t *testing.T, d *daemon.Daemon, sessionID, fixture string) {
	t.Helper()
	t.Setenv("HIVE_SESSION_ID", sessionID)
	t.Setenv("HIVE_SOCKET", d.SocketPath())
	raw := readFixture(t, fixture)
	runHook(bytesReader(raw))
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// TestHookIntegrationDrivesRealSession runs every hook fixture through
// the real code path (mapHookPayload → ModeEvent socket write →
// daemon's ApplyAgentEvent) against a real daemon and a real session,
// and asserts the session's State/StateSource/LastPrompt/LastSummary
// via the client list, plus needs_attention flipping on waiting_*.
func TestHookIntegrationDrivesRealSession(t *testing.T) {
	d := startHookTestDaemon(t)
	list := d.Registry().List()
	if len(list) == 0 {
		t.Fatal("no bootstrap session")
	}
	id := list[0].ID

	wait := func(cond func(wire.SessionInfo) bool, what string) wire.SessionInfo {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			info, ok := findSessionByID(d, id)
			if ok && cond(info) {
				return info
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s; last info = %+v", what, info)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// UserPromptSubmit -> working, LastPrompt set.
	runHookFixture(t, d, id, "user_prompt_submit.json")
	wait(func(i wire.SessionInfo) bool {
		return i.State == wire.StateWorking && i.StateSource == wire.StateSourceHook && i.LastPrompt == "reply pong"
	}, "prompt applied")

	// Stop -> idle, LastSummary set, and under the derived model this
	// is NOT a wait (see the plan's phase-2 follow-up note) so
	// needs_attention stays false here.
	runHookFixture(t, d, id, "stop.json")
	wait(func(i wire.SessionInfo) bool {
		return i.State == wire.StateIdle && i.LastSummary == "pong"
	}, "turn_end applied")

	// PermissionRequest -> waiting_permission, needs_attention true.
	runHookFixture(t, d, id, "permission_request.json")
	info := wait(func(i wire.SessionInfo) bool {
		return i.State == wire.StateWaitingPermission
	}, "waiting_permission applied")
	if !info.NeedsAttention {
		t.Errorf("NeedsAttention = false, want true for waiting_permission")
	}

	// PostToolUse -> permission_resolved (working again).
	runHookFixture(t, d, id, "post_tool_use.json")
	info = wait(func(i wire.SessionInfo) bool {
		return i.State == wire.StateWorking
	}, "permission_resolved applied")
	if info.NeedsAttention {
		t.Errorf("NeedsAttention = true, want false once resolved")
	}

	// StopFailure -> error, LastSummary set to the error type.
	runHookFixture(t, d, id, "stop_failure.json")
	wait(func(i wire.SessionInfo) bool {
		return i.State == wire.StateError && i.LastSummary == "overloaded"
	}, "error applied")
}

// TestHookIntegrationBroadcastsStateEvent asserts a control connection
// actually receives SESSION_EVENT(state) for a hook-driven transition —
// the plumbing from ApplyAgentEvent through announceStateLocked to the
// registry's broadcast fan-out.
func TestHookIntegrationBroadcastsStateEvent(t *testing.T) {
	d := startHookTestDaemon(t)
	list := d.Registry().List()
	id := list[0].ID

	conn, events := dialControlAndSubscribe(t, d)
	defer conn.Close()

	runHookFixture(t, d, id, "user_prompt_submit.json")

	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed before seeing state event")
			}
			if ev.Kind == wire.SessionEventState && ev.Session.ID == id {
				return
			}
		case <-time.After(time.Until(deadline)):
			t.Fatal("timed out waiting for SESSION_EVENT(state)")
		}
	}
}
