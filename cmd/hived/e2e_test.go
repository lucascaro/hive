//go:build e2e

// Layer A end-to-end tests: spawn the real hived binary as a
// subprocess against an isolated temp socket + state dir, and exercise
// multi-session flows end-to-end via the binary wire protocol.
//
// Run: go test -tags=e2e ./cmd/hived/...
//
// These tests are the regression net for the daemon ↔ wire layer that
// the GUI's mock-Wails Playwright suite cannot see. If you change
// scrollback / fanout / persistence semantics, a test here should
// fail; if none did, the test coverage is incomplete.
//
// Isolation: every test sets HIVE_SOCKET and HIVE_STATE_DIR to temp
// paths and the testclient package's RequireIsolation guard refuses
// to dial otherwise. Production state is never touched.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/wire/testclient"
)

// --- shared binary build (once per package run) ---

var (
	binPath     string
	binDir      string
	binBuildErr error
	binOnce     sync.Once
)

// TestMain owns the package-level cleanup for the build dir created
// by hivedBinary. Without it every `go test -tags=e2e` run left a
// stray /tmp/hived-bin*/ behind, which accumulates over time on dev
// machines and CI runners alike.
func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// hivedBinary builds the cmd/hived binary into a temp path once per
// test process and returns the path. Subsequent tests reuse it. The
// directory is recorded in binDir so TestMain can remove it on exit.
func hivedBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("/tmp", "hived-bin")
		if err != nil {
			binBuildErr = fmt.Errorf("mkdir: %w", err)
			return
		}
		out := filepath.Join(dir, "hived")
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", out, "./cmd/hived")
		cmd.Dir = repoRoot(t)
		cmd.Env = os.Environ()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			binBuildErr = fmt.Errorf("go build hived: %v\n%s", err, stderr.String())
			return
		}
		binPath = out
		binDir = dir
	})
	if binBuildErr != nil {
		t.Fatalf("hivedBinary: %v", binBuildErr)
	}
	return binPath
}

// repoRoot walks up from the current test file until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repoRoot: no go.mod above %s", wd)
		}
		dir = parent
	}
}

// --- daemon spawn helper ---

type spawnedDaemon struct {
	cmd      *exec.Cmd
	sockPath string
	stateDir string
	stderr   *bytes.Buffer
}

func skipIfWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon e2e requires POSIX shell")
	}
}

// spawnDaemon starts hived with isolated HIVE_SOCKET + HIVE_STATE_DIR,
// waits for the socket to appear, and registers cleanup.
func spawnDaemon(t *testing.T) *spawnedDaemon {
	t.Helper()
	skipIfWindows(t)

	tmp, err := os.MkdirTemp("/tmp", "hived-e2e")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	sock := filepath.Join(tmp, "h.sock")
	state := filepath.Join(tmp, "state")

	// Isolation env vars BEFORE the spawn — they're what we're enforcing.
	t.Setenv("HIVE_SOCKET", sock)
	t.Setenv("HIVE_STATE_DIR", state)
	// HOME isolation: the daemon writes worktrees and may read git
	// config; pin HOME to the temp dir too.
	t.Setenv("HOME", tmp)

	if err := testclient.RequireIsolation(); err != nil {
		t.Fatalf("isolation guard: %v", err)
	}

	d := startBinary(t, sock, state, tmp)
	waitForSocket(t, sock, 5*time.Second)
	return d
}

func startBinary(t *testing.T, sock, state, home string) *spawnedDaemon {
	t.Helper()
	bin := hivedBinary(t)
	cmd := exec.Command(bin, "--socket", sock, "--shell", "/bin/bash", "--cols", "80", "--rows", "24")
	cmd.Env = append(os.Environ(),
		"HIVE_SOCKET="+sock,
		"HIVE_STATE_DIR="+state,
		"HOME="+home,
		"TERM=dumb",
	)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hived: %v", err)
	}
	d := &spawnedDaemon{cmd: cmd, sockPath: sock, stateDir: state, stderr: stderr}
	t.Cleanup(func() { d.stop(t) })
	return d
}

func (d *spawnedDaemon) stop(t *testing.T) {
	t.Helper()
	if d.cmd == nil || d.cmd.Process == nil {
		return
	}
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = d.cmd.Process.Kill()
		<-done
	}
	if t.Failed() && d.stderr.Len() > 0 {
		t.Logf("hived stderr:\n%s", d.stderr.String())
	}
}

func waitForSocket(t *testing.T, sock string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("hived socket %s did not appear within %s", sock, timeout)
}

// dialControl opens a control-mode client and drains initial snapshots.
func dialControl(t *testing.T, d *spawnedDaemon) *testclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := testclient.Dial(ctx, d.sockPath)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Handshake(wire.Hello{Mode: wire.ModeControl, Client: "e2e/control"}); err != nil {
		t.Fatalf("control handshake: %v", err)
	}
	if err := c.DrainInitialSnapshots(3 * time.Second); err != nil {
		t.Fatalf("drain snapshots: %v", err)
	}
	return c
}

// dialAttach opens an attach-mode client and drains the initial replay.
func dialAttach(t *testing.T, d *spawnedDaemon, sessionID string) *testclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := testclient.Dial(ctx, d.sockPath)
	if err != nil {
		t.Fatalf("dial attach: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Handshake(wire.Hello{Mode: wire.ModeAttach, SessionID: sessionID, Client: "e2e/attach"}); err != nil {
		t.Fatalf("attach handshake: %v", err)
	}
	if _, err := c.AwaitReplayBoundary(3 * time.Second); err != nil {
		t.Fatalf("initial replay: %v", err)
	}
	return c
}

// firstSession queries the bootstrap session ID via control mode.
func firstSession(t *testing.T, d *spawnedDaemon) string {
	t.Helper()
	c := dialControl(t, d)
	if err := c.ListSessions(); err != nil {
		t.Fatalf("list: %v", err)
	}
	snap, err := c.AwaitSessionsSnapshot(2 * time.Second)
	if err != nil {
		t.Fatalf("snap: %v", err)
	}
	if len(snap.Sessions) == 0 {
		t.Fatalf("no sessions in daemon")
	}
	return snap.Sessions[0].ID
}

// --- tests ---

// TestE2E_SessionLifecycle: bootstrap session, attach, run echo, detach,
// reattach, scrollback replay carries the marker.
func TestE2E_SessionLifecycle(t *testing.T) {
	d := spawnDaemon(t)
	id := firstSession(t, d)

	a := dialAttach(t, d, id)
	if err := a.WriteStdin([]byte("echo HIVE_E2E_MARK_$((1+2))\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if _, err := a.WaitForData([]byte("HIVE_E2E_MARK_3"), 3*time.Second); err != nil {
		t.Fatalf("did not see live marker: %v", err)
	}
	_ = a.Close()

	// Reattach in a fresh client; replay must include the marker.
	b := dialAttachNoReplayDrain(t, d, id)
	defer b.Close()
	replay, err := b.AwaitReplayBoundary(3 * time.Second)
	if err != nil {
		t.Fatalf("await replay: %v", err)
	}
	if !bytes.Contains(replay, []byte("HIVE_E2E_MARK_3")) {
		t.Errorf("replay missing marker (%d bytes): %q", len(replay), replay)
	}
}

// dialAttachNoReplayDrain is like dialAttach but doesn't consume the
// initial replay — used when the test itself wants to inspect it.
func dialAttachNoReplayDrain(t *testing.T, d *spawnedDaemon, sessionID string) *testclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := testclient.Dial(ctx, d.sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Handshake(wire.Hello{Mode: wire.ModeAttach, SessionID: sessionID, Client: "e2e/attach-raw"}); err != nil {
		t.Fatalf("attach handshake: %v", err)
	}
	return c
}

// TestE2E_ScrollbackAtomicityUnderConcurrentFanout: two attach clients
// on the same session, write a high-volume burst, both must receive
// identical live byte streams. Catches snapshot-vs-fanout races
// (regression class behind #200 / #203).
func TestE2E_ScrollbackAtomicityUnderConcurrentFanout(t *testing.T) {
	d := spawnDaemon(t)
	id := firstSession(t, d)

	a := dialAttach(t, d, id)
	b := dialAttach(t, d, id)

	// Use a third attach to drive stdin so the two observers are
	// pure readers.
	driver := dialAttach(t, d, id)

	// Disable tty echo so the typed command doesn't appear in either
	// observer's stream — otherwise the START/END markers would match
	// in the echoed input before any output is produced.
	if err := driver.WriteStdin([]byte("stty -echo\n")); err != nil {
		t.Fatalf("disable echo: %v", err)
	}
	// Give bash a moment to apply stty before issuing the burst.
	time.Sleep(150 * time.Millisecond)

	// Drain any pre-burst noise on the observers so the clip window
	// starts cleanly.
	for _, cli := range []*testclient.Client{a, b} {
		_, _ = cli.WaitForData([]byte("\n"), 200*time.Millisecond)
	}

	const lines = 200
	const start = "HIVE_BURST_START_44"
	const end = "HIVE_BURST_END_99"
	burst := fmt.Sprintf("printf '%s\\n'; for i in $(seq 1 %d); do printf 'L%%04d\\n' $i; done; printf '%s\\n'\n",
		start, lines, end)
	if err := driver.WriteStdin([]byte(burst)); err != nil {
		t.Fatalf("driver write: %v", err)
	}
	_ = driver.Close()

	got := make([][]byte, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i, cli := range []*testclient.Client{a, b} {
		i, cli := i, cli
		go func() {
			defer wg.Done()
			out, err := cli.WaitForData([]byte(end), 8*time.Second)
			if err != nil {
				t.Errorf("client %d: %v", i, err)
				return
			}
			got[i] = clipBetween(out, []byte(start), []byte(end))
		}()
	}
	wg.Wait()

	if got[0] == nil || got[1] == nil {
		t.Fatalf("at least one attach client failed to see end marker")
	}
	if !bytes.Equal(got[0], got[1]) {
		// Diff first divergence position to make the failure
		// actionable.
		t.Errorf("fanout streams diverged (len a=%d b=%d, first diff at %d)",
			len(got[0]), len(got[1]), firstDiff(got[0], got[1]))
	}
	// Sanity-check: the clipped window includes both the first AND
	// last burst lines. Without checking the last line, a truncated
	// burst that contained only the first few lines would still pass
	// the equality check above as long as both observers were
	// truncated identically — and the equality check is the whole
	// point of the test. The end-to-end count assertion guarantees
	// that "both got the same bytes" actually means "both got the
	// full payload".
	for _, want := range []string{"L0001", fmt.Sprintf("L%04d", lines)} {
		if !strings.Contains(string(got[0]), want) {
			t.Errorf("burst content missing %q on stream A (len=%d): %q",
				want, len(got[0]), truncate(got[0], 200))
		}
	}
}

// truncate returns the first n bytes of b followed by "..." when
// truncated. Keeps test failure messages readable without dumping
// kilobytes of bash output.
func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	out := make([]byte, 0, n+3)
	out = append(out, b[:n]...)
	out = append(out, '.', '.', '.')
	return out
}

// clipBetween extracts the substring after `start` and up to (not
// including) `end`. Returns nil if either marker is missing.
func clipBetween(b, start, end []byte) []byte {
	i := bytes.Index(b, start)
	if i < 0 {
		return nil
	}
	rest := b[i+len(start):]
	j := bytes.Index(rest, end)
	if j < 0 {
		return nil
	}
	return rest[:j]
}

func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// TestE2E_MultiSessionIsolation: four sessions, distinct payloads
// written concurrently, no cross-talk between scrollbacks.
func TestE2E_MultiSessionIsolation(t *testing.T) {
	d := spawnDaemon(t)
	ctl := dialControl(t, d)

	const n = 4
	ids := make([]string, n)
	for i := range n {
		if err := ctl.CreateSession(wire.CreateSpec{
			Name:  fmt.Sprintf("iso-%d", i),
			Cols:  80,
			Rows:  24,
			Shell: "/bin/bash",
		}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ev, err := ctl.AwaitSessionEvent(wire.SessionEventAdded, 3*time.Second)
		if err != nil {
			t.Fatalf("await added %d: %v", i, err)
		}
		ids[i] = ev.Session.ID
	}

	var wg sync.WaitGroup
	wg.Add(n)
	var failures atomic.Int32
	for i := range n {
		i := i
		go func() {
			defer wg.Done()
			c := dialAttach(t, d, ids[i])
			marker := fmt.Sprintf("HIVE_ISO_MARK_%d", i)
			if err := c.WriteStdin([]byte("echo " + marker + "\n")); err != nil {
				t.Errorf("write %d: %v", i, err)
				failures.Add(1)
				return
			}
			out, err := c.WaitForData([]byte(marker), 5*time.Second)
			if err != nil {
				t.Errorf("wait %d: %v", i, err)
				failures.Add(1)
				return
			}
			// Cross-talk check: no other session's marker should
			// appear in this session's stream.
			for j := range n {
				if j == i {
					continue
				}
				other := fmt.Sprintf("HIVE_ISO_MARK_%d", j)
				if bytes.Contains(out, []byte(other)) {
					t.Errorf("session %d saw foreign marker %s in its stream", i, other)
					failures.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if failures.Load() > 0 {
		t.Fatalf("multi-session isolation: %d failures", failures.Load())
	}
}

// TestE2E_DaemonRestart: create a named session, SIGTERM the daemon,
// re-spawn against the same state dir, verify the persisted session
// is restored AND its PTY is alive again (the daemon's startup loop
// calls reg.Revive on every persisted entry — see
// internal/daemon/daemon.go:99 — which spawns a fresh shell).
func TestE2E_DaemonRestart(t *testing.T) {
	d := spawnDaemon(t)
	ctl := dialControl(t, d)

	// Create a named session so we can identify it post-restart.
	const sessName = "survivor"
	if err := ctl.CreateSession(wire.CreateSpec{
		Name: sessName, Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ev, err := ctl.AwaitSessionEvent(wire.SessionEventAdded, 3*time.Second)
	if err != nil {
		t.Fatalf("await added: %v", err)
	}
	createdID := ev.Session.ID
	_ = ctl.Close()

	// Kill the daemon (SIGTERM) and wait for shutdown.
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit after SIGTERM")
	}
	// Socket file should be unlinked on clean shutdown.
	if _, err := os.Stat(d.sockPath); err == nil {
		// Best-effort cleanup so the new daemon can bind.
		_ = os.Remove(d.sockPath)
	}

	// Restart against the same state dir.
	d.cmd = nil // suppress duplicate stop in t.Cleanup
	d2 := startBinary(t, d.sockPath, d.stateDir, filepath.Dir(d.sockPath))
	waitForSocket(t, d2.sockPath, 5*time.Second)

	ctl2 := dialControl(t, d2)
	if err := ctl2.ListSessions(); err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	snap, err := ctl2.AwaitSessionsSnapshot(3 * time.Second)
	if err != nil {
		t.Fatalf("snapshot after restart: %v", err)
	}
	var restored *wire.SessionInfo
	for i := range snap.Sessions {
		if snap.Sessions[i].ID == createdID && snap.Sessions[i].Name == sessName {
			restored = &snap.Sessions[i]
			break
		}
	}
	if restored == nil {
		t.Fatalf("session %s (%s) did not survive daemon restart; saw %+v", createdID, sessName, snap.Sessions)
	}
	if !restored.Alive {
		t.Errorf("session %s should be alive after restart (daemon Revives), got alive=false: %+v",
			createdID, *restored)
	}
}

// TestE2E_ProtocolVersionMismatch: connect with a bogus wire version
// and verify the daemon refuses with ERROR rather than crashing.
func TestE2E_ProtocolVersionMismatch(t *testing.T) {
	d := spawnDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := testclient.Dial(ctx, d.sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_, err = c.Handshake(wire.Hello{Version: 999, Mode: wire.ModeControl, Client: "e2e/bad"})
	if err == nil {
		t.Fatal("expected handshake error on bogus version")
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("expected 'refused' in error, got %v", err)
	}
}

// TestE2E_IsolationGuard_FailsClosed: the testclient must refuse to
// dial when isolation env vars are missing. This is the test of the
// guard itself, not of the daemon.
func TestE2E_IsolationGuard_FailsClosed(t *testing.T) {
	t.Setenv("HIVE_SOCKET", "")
	t.Setenv("HIVE_STATE_DIR", "")
	if err := testclient.RequireIsolation(); err == nil {
		t.Fatal("RequireIsolation should fail when env unset")
	}
}
