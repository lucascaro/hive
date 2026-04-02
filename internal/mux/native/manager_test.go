//go:build !windows

package muxnative

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestManager returns a fresh manager (not the package singleton) for tests.
func newTestManager() *manager {
	return &manager{sessions: make(map[string]*muxSession)}
}

// --- createSession / sessionExists / killSession ----------------------------

func TestCreateAndExistsSession(t *testing.T) {
	mgr := newTestManager()
	err := mgr.createSession("sess1", "win1", t.TempDir(), []string{"sh", "-c", "sleep 60"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	if !mgr.sessionExists("sess1") {
		t.Fatal("expected session to exist after creation")
	}
}

func TestCreateSessionIdempotent(t *testing.T) {
	mgr := newTestManager()
	dir := t.TempDir()
	if err := mgr.createSession("s", "w", dir, []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	// Second call should be a no-op (session already exists).
	if err := mgr.createSession("s", "w", dir, []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatalf("second createSession should be no-op, got: %v", err)
	}
}

func TestKillSession(t *testing.T) {
	mgr := newTestManager()
	if err := mgr.createSession("k", "w", t.TempDir(), []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.killSession("k"); err != nil {
		t.Fatalf("killSession: %v", err)
	}
	if mgr.sessionExists("k") {
		t.Error("session should not exist after kill")
	}
}

func TestKillSessionNotFound(t *testing.T) {
	mgr := newTestManager()
	err := mgr.killSession("no-such-session")
	if err == nil {
		t.Fatal("expected error killing non-existent session")
	}
}

// --- createWindow -----------------------------------------------------------

func TestCreateWindow(t *testing.T) {
	mgr := newTestManager()
	if err := mgr.createSession("s", "win0", t.TempDir(), []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	idx, err := mgr.createWindow("s", "win1", t.TempDir(), []string{"sh", "-c", "sleep 60"})
	if err != nil {
		t.Fatalf("createWindow: %v", err)
	}
	if idx < 1 {
		t.Errorf("expected idx >= 1, got %d", idx)
	}
}

func TestCreateWindowSessionNotFound(t *testing.T) {
	mgr := newTestManager()
	_, err := mgr.createWindow("ghost", "w", t.TempDir(), []string{"sh", "-c", "sleep 60"})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

// TestCreateWindowRace verifies that createWindow does not panic or leave
// an orphaned pane when killSession races with it.
func TestCreateWindowRace(t *testing.T) {
	const iterations = 50
	for i := range iterations {
		mgr := newTestManager()
		sessName := fmt.Sprintf("race-sess-%d", i)
		if err := mgr.createSession(sessName, "win0", t.TempDir(), []string{"sh", "-c", "sleep 60"}); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// goroutine 1: create a new window
		go func() {
			defer wg.Done()
			mgr.createWindow(sessName, "win1", t.TempDir(), []string{"sh", "-c", "sleep 60"}) //nolint:errcheck
		}()

		// goroutine 2: kill the session concurrently
		go func() {
			defer wg.Done()
			mgr.killSession(sessName) //nolint:errcheck
		}()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock detected in createWindow/killSession race")
		}
	}
}

// --- listSessionNames -------------------------------------------------------

func TestListSessionNames(t *testing.T) {
	mgr := newTestManager()
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := mgr.createSession(name, "w", dir, []string{"sh", "-c", "sleep 60"}); err != nil {
			t.Fatal(err)
		}
	}
	names := mgr.listSessionNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(names))
	}
}

// --- windowExists / killWindow / renameWindow -------------------------------

func TestWindowExists(t *testing.T) {
	mgr := newTestManager()
	if err := mgr.createSession("s", "win0", t.TempDir(), []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	target := "s:0"
	if !mgr.windowExists(target) {
		t.Errorf("expected window %s to exist", target)
	}
}

func TestKillWindow(t *testing.T) {
	mgr := newTestManager()
	if err := mgr.createSession("s", "win0", t.TempDir(), []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.killWindow("s:0"); err != nil {
		t.Fatalf("killWindow: %v", err)
	}
	if mgr.windowExists("s:0") {
		t.Error("window should not exist after kill")
	}
}

func TestRenameWindow(t *testing.T) {
	mgr := newTestManager()
	if err := mgr.createSession("s", "original", t.TempDir(), []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.renameWindow("s:0", "renamed"); err != nil {
		t.Fatalf("renameWindow: %v", err)
	}
	wins, err := mgr.listWindows("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 1 || wins[0].name != "renamed" {
		t.Errorf("expected window named 'renamed', got %+v", wins)
	}
}

// --- pane.kill nil-safety --------------------------------------------------

func TestPaneKillNilSafe(t *testing.T) {
	// A zero-value pane should not panic when killed.
	p := &pane{}
	p.kill() // must not panic
}

// --- parseTarget -----------------------------------------------------------

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in      string
		sess    string
		idx     int
		wantErr bool
	}{
		{"mySession:3", "mySession", 3, false},
		{"a:b:2", "a:b", 2, false},   // session name may contain colons
		{"nocolon", "", 0, true},
		{"s:notnum", "", 0, true},
	}
	for _, tc := range cases {
		sess, idx, err := parseTarget(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTarget(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTarget(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if sess != tc.sess || idx != tc.idx {
			t.Errorf("parseTarget(%q): got (%q, %d), want (%q, %d)", tc.in, sess, idx, tc.sess, tc.idx)
		}
	}
}

// --- lastLines helper -------------------------------------------------------

func TestLastLines(t *testing.T) {
	input := "a\nb\nc\nd\ne"
	if got := lastLines(input, 3); got != "c\nd\ne" {
		t.Errorf("lastLines(3) = %q", got)
	}
	if got := lastLines(input, 0); got != input {
		t.Errorf("lastLines(0) = %q", got)
	}
	if got := lastLines(input, 100); got != input {
		t.Errorf("lastLines(100) = %q", got)
	}
}

// --- capture ----------------------------------------------------------------

func TestPaneCapture(t *testing.T) {
	mgr := newTestManager()
	// Use echo so the process produces output then exits.
	if err := mgr.createSession("cap", "w", t.TempDir(), []string{"sh", "-c", `printf 'hello\nworld\n'`}); err != nil {
		t.Fatal(err)
	}
	// Give the process a moment to write its output.
	time.Sleep(200 * time.Millisecond)

	p := mgr.paneByTarget("cap:0")
	if p == nil {
		t.Fatal("pane not found")
	}
	out := p.capture(0)
	if !strings.Contains(out, "hello") {
		t.Errorf("capture output does not contain 'hello': %q", out)
	}
}
