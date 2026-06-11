package registry

import (
	"bytes"
	"log"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// captureLog redirects the stdlib logger into a buffer for the duration
// of the test. The registry logs diagnosability warnings through it.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

// skipIfChmodIneffective skips tests that inject persist failures by
// revoking directory write permission — meaningless on Windows and as
// root, where permission bits don't deny access.
func skipIfChmodIneffective(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based failure injection requires POSIX permissions")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
}

func TestPersistLoggedHelpersWarnOnFailure(t *testing.T) {
	skipIfChmodIneffective(t)
	stateDir := t.TempDir()
	r, err := Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	sessDir := SessionsDir(stateDir)
	projDir := ProjectsDir(stateDir)
	for _, d := range []string{sessDir, projDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatalf("Chmod %s: %v", d, err)
		}
		t.Cleanup(func() { _ = os.Chmod(d, 0o700) })
	}

	cases := []struct {
		name string
		call func()
		want string
	}{
		{
			name: "entry",
			call: func() { r.persistEntryLoggedLocked(&Entry{ID: "test-entry"}, "test-op") },
			want: "test-op: persist session test-entry failed",
		},
		{
			name: "session index",
			call: func() { r.persistIndexLoggedLocked("test-op") },
			want: "test-op: persist session index failed",
		},
		{
			name: "project",
			call: func() { r.persistProjectLoggedLocked(&Project{ID: "test-proj"}, "test-op") },
			want: "test-op: persist project test-proj failed",
		},
		{
			name: "project index",
			call: func() { r.persistProjectIndexLoggedLocked("test-op") },
			want: "test-op: persist project index failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			// The *Locked helpers are called under r.mu everywhere in
			// production — honor the same contract here.
			r.mu.Lock()
			tc.call()
			r.mu.Unlock()
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("expected log containing %q; got: %q", tc.want, buf.String())
			}
		})
	}
}

func TestPersistLoggedHelpersSilentOnSuccess(t *testing.T) {
	stateDir := t.TempDir()
	r, err := Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	buf := captureLog(t)
	// The *Locked helpers are called under r.mu everywhere in
	// production — honor the same contract here.
	r.mu.Lock()
	r.persistEntryLoggedLocked(&Entry{ID: "ok-entry"}, "test-op")
	r.persistIndexLoggedLocked("test-op")
	r.persistProjectLoggedLocked(&Project{ID: "ok-proj"}, "test-op")
	r.persistProjectIndexLoggedLocked("test-op")
	r.mu.Unlock()
	if got := buf.String(); strings.Contains(got, "persist") {
		t.Errorf("happy path should log nothing; got: %q", got)
	}
}

func TestBroadcastDropsAndWarnsOnSlowListener(t *testing.T) {
	r := freshRegistry(t)
	buf := captureLog(t)

	slow, unsubSlow := r.Subscribe()
	storm := cap(slow) + 1
	for range storm {
		r.broadcast(wire.SessionEventUpdated, wire.SessionInfo{ID: "storm"})
	}

	if !strings.Contains(buf.String(), "dropping slow session-event listener") {
		t.Errorf("expected slow-listener warning; got: %q", buf.String())
	}

	// The dropped channel holds cap(slow) buffered events and must then
	// be closed — that is the signal a client uses to resubscribe.
	closed := false
	deadline := time.After(2 * time.Second)
	for !closed {
		select {
		case _, ok := <-slow:
			if !ok {
				closed = true
			}
		case <-deadline:
			t.Fatal("dropped listener channel was never closed")
		}
	}

	// Unsubscribing after the drop must be a harmless no-op (no double
	// close panic).
	unsubSlow()

	// The registry must keep serving listeners subscribed after a drop.
	fresh, unsubFresh := r.Subscribe()
	defer unsubFresh()
	r.broadcast(wire.SessionEventUpdated, wire.SessionInfo{ID: "after-drop"})
	select {
	case ev := <-fresh:
		if ev.Session.ID != "after-drop" {
			t.Errorf("fresh listener got %q, want %q", ev.Session.ID, "after-drop")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fresh listener received nothing after slow-listener drop")
	}
}

func TestBroadcastProjectDropsAndWarns(t *testing.T) {
	r := freshRegistry(t)
	buf := captureLog(t)

	slow, unsubSlow := r.SubscribeProjects()
	storm := cap(slow) + 1
	for range storm {
		r.broadcastProject(wire.ProjectEventUpdated, wire.ProjectInfo{ID: "storm"})
	}

	if !strings.Contains(buf.String(), "dropping slow project-event listener") {
		t.Errorf("expected slow-listener warning; got: %q", buf.String())
	}

	closed := false
	deadline := time.After(2 * time.Second)
	for !closed {
		select {
		case _, ok := <-slow:
			if !ok {
				closed = true
			}
		case <-deadline:
			t.Fatal("dropped project listener channel was never closed")
		}
	}
	unsubSlow()
}
