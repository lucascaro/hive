package daemon

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// shortTempDir creates a temp dir under /tmp (short path) so that the
// Unix-socket path stays under macOS's 104-character sun_path limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon E2E test requires POSIX shell")
	}
}

// startTestDaemon brings up a daemon with a bootstrap session and
// returns it. State directory is also temporary.
func startTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	tmp := shortTempDir(t)
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash",
			Cols:  80,
			Rows:  24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = d.Close()
	})
	return d
}

func dial(t *testing.T, d *Daemon) net.Conn {
	t.Helper()
	c, err := net.Dial("unix", d.SocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func handshake(t *testing.T, conn net.Conn, hello wire.Hello) wire.Welcome {
	t.Helper()
	hello.Version = wire.PROTOCOL_VERSION
	if hello.Client == "" {
		hello.Client = "test/0"
	}
	if err := wire.WriteJSON(conn, wire.FrameHello, hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var w wire.Welcome
	ft, err := wire.ReadJSON(conn, &w)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if ft == wire.FrameError {
		t.Fatalf("got ERROR during handshake")
	}
	if ft != wire.FrameWelcome {
		t.Fatalf("expected WELCOME, got %s", ft)
	}
	return w
}

// readUntilReplayDone consumes DATA frames into out until the
// scrollback_replay_done event arrives.
func readUntilReplayDone(t *testing.T, conn net.Conn, out *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		switch ft {
		case wire.FrameData:
			out.Write(payload)
		case wire.FrameEvent:
			return
		default:
			t.Fatalf("unexpected frame during replay: %s", ft)
		}
	}
}

func drainFor(conn net.Conn, out *bytes.Buffer, d time.Duration) {
	deadline := time.Now().Add(d)
	for {
		_ = conn.SetReadDeadline(deadline)
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return
		}
		if ft == wire.FrameData {
			out.Write(payload)
		}
	}
}

// firstSessionID grabs the bootstrap session via the registry helper.
func firstSessionID(t *testing.T, d *Daemon) string {
	t.Helper()
	list := d.Registry().List()
	if len(list) == 0 {
		t.Fatalf("registry has no sessions")
	}
	return list[0].ID
}

func TestAttachReattachReplay(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := firstSessionID(t, d)

	// First attach.
	c1 := dial(t, d)
	w1 := handshake(t, c1, wire.Hello{Mode: wire.ModeAttach, SessionID: id})
	if w1.SessionID != id {
		t.Fatalf("welcome session id: got %s, want %s", w1.SessionID, id)
	}
	var buf1 bytes.Buffer
	readUntilReplayDone(t, c1, &buf1)
	if err := wire.WriteFrame(c1, wire.FrameData, []byte("echo HIVE_PROBE_$((20+22))\n")); err != nil {
		t.Fatalf("write data: %v", err)
	}
	drainFor(c1, &buf1, 800*time.Millisecond)
	if !strings.Contains(buf1.String(), "HIVE_PROBE_42") {
		t.Fatalf("missing marker on first attach: %q", buf1.String())
	}
	_ = c1.Close()

	// Reattach.
	c2 := dial(t, d)
	_ = handshake(t, c2, wire.Hello{Mode: wire.ModeAttach, SessionID: id})
	var replay bytes.Buffer
	readUntilReplayDone(t, c2, &replay)
	if !bytes.Contains(replay.Bytes(), []byte("HIVE_PROBE_42")) {
		t.Errorf("replay missing prior output: %q", replay.String())
	}
	_ = c2.Close()
}

func TestControlListAndCreate(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})

	// Initial snapshots arrive unsolicited from the daemon: PROJECTS
	// first (so the client can resolve session.project_id), then
	// SESSIONS.
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read project snapshot: %v", err)
	}
	if ft != wire.FrameProjects {
		t.Fatalf("expected PROJECTS snapshot, got %s", ft)
	}
	var psnap wire.ProjectsResp
	_ = jsonUnmarshal(payload, &psnap)
	if len(psnap.Projects) != 1 || psnap.Projects[0].Name != "default" {
		t.Errorf("project snapshot: %+v", psnap)
	}

	ft, payload, err = wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if ft != wire.FrameSessions {
		t.Fatalf("expected SESSIONS snapshot, got %s", ft)
	}
	var snap wire.SessionsResp
	_ = jsonUnmarshal(payload, &snap)
	if len(snap.Sessions) != 1 || snap.Sessions[0].Name != "main" {
		t.Errorf("snapshot: %+v", snap)
	}
	if snap.Sessions[0].ProjectID == "" {
		t.Errorf("session snapshot missing ProjectID")
	}

	// Create a new session.
	if err := wire.WriteJSON(conn, wire.FrameCreateSession, wire.CreateSpec{
		Name: "extra", Color: "#abc", Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Expect a SESSION_EVENT added.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	ft, payload, err = wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if ft != wire.FrameSessionEvent {
		t.Fatalf("expected SESSION_EVENT, got %s", ft)
	}
	var ev wire.SessionEvent
	_ = jsonUnmarshal(payload, &ev)
	if ev.Kind != wire.SessionEventAdded || ev.Session.Name != "extra" {
		t.Errorf("event: %+v", ev)
	}

	// LIST_SESSIONS now sees both.
	_ = wire.WriteJSON(conn, wire.FrameListSessions, wire.ListSessionsReq{})
	ft, payload, err = wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read list: %v", err)
	}
	if ft != wire.FrameSessions {
		t.Fatalf("expected SESSIONS, got %s", ft)
	}
	_ = jsonUnmarshal(payload, &snap)
	if len(snap.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %+v", snap)
	}
}

func TestCreateModeAttachesToNewSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	w := handshake(t, conn, wire.Hello{
		Mode: wire.ModeCreate,
		Create: &wire.CreateSpec{
			Name: "fresh", Cols: 80, Rows: 24, Shell: "/bin/bash",
		},
	})
	if w.SessionID == "" {
		t.Fatalf("welcome did not carry session id")
	}
	var buf bytes.Buffer
	readUntilReplayDone(t, conn, &buf)
	// New session: no prior scrollback.
	if buf.Len() > 256 {
		t.Errorf("unexpectedly large replay on new session: %d bytes", buf.Len())
	}
	if err := wire.WriteFrame(conn, wire.FrameData, []byte("echo created\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	drainFor(conn, &buf, 800*time.Millisecond)
	if !strings.Contains(buf.String(), "created") {
		t.Errorf("expected 'created' echo: %q", buf.String())
	}
	// Registry now has 2 sessions.
	if got := len(d.Registry().List()); got != 2 {
		t.Errorf("expected 2 sessions in registry, got %d", got)
	}
}

func TestControlProjectsRoundTrip(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})

	// Drain initial PROJECTS + SESSIONS snapshots.
	for i := 0; i < 2; i++ {
		if _, _, err := wire.ReadFrame(conn); err != nil {
			t.Fatalf("drain snapshot %d: %v", i, err)
		}
	}

	// Create a new project.
	if err := wire.WriteJSON(conn, wire.FrameCreateProject, wire.CreateProjectReq{
		Name: "alpha", Color: "#abc", Cwd: "/tmp",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Expect a PROJECT_EVENT(added).
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read project event: %v", err)
	}
	if ft != wire.FrameProjectEvent {
		t.Fatalf("expected PROJECT_EVENT, got %s", ft)
	}
	var ev wire.ProjectEvent
	_ = jsonUnmarshal(payload, &ev)
	if ev.Kind != wire.ProjectEventAdded || ev.Project.Name != "alpha" {
		t.Errorf("project event: %+v", ev)
	}

	// LIST_PROJECTS should now return both projects.
	_ = wire.WriteJSON(conn, wire.FrameListProjects, wire.ListProjectsReq{})
	ft, payload, err = wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read projects: %v", err)
	}
	if ft != wire.FrameProjects {
		t.Fatalf("expected PROJECTS, got %s", ft)
	}
	var resp wire.ProjectsResp
	_ = jsonUnmarshal(payload, &resp)
	if len(resp.Projects) != 2 {
		t.Errorf("expected 2 projects after create, got %d (%+v)", len(resp.Projects), resp)
	}
}

// TestKill_DirtyWorktree_FrameError verifies the daemon translates
// registry.ErrWorktreeDirty into a wire.FrameError with the
// well-known "worktree_dirty" code so the GUI can confirm with the
// user. Force=true on a retry succeeds.
func TestKill_DirtyWorktree_FrameError(t *testing.T) {
	skipOnWindows(t)
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		// We don't *need* /usr/bin/git specifically; PATH lookup is fine.
	}
	// Bring the daemon up against a fresh temp git repo as the
	// project cwd so created sessions get worktrees.
	tmp := shortTempDir(t)
	repo := filepath.Join(tmp, "repo")
	_ = os.MkdirAll(repo, 0o755)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash", Cols: 80, Rows: 24,
			Cwd: repo,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() { cancel(); _ = d.Close() })

	// Open control conn; create a worktree-backed session.
	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	// Drain initial PROJECTS + SESSIONS snapshots (and any added events).
	deadline := time.Now().Add(2 * time.Second)
	var sessionID string
	for time.Now().Before(deadline) && sessionID == "" {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			break
		}
		if ft == wire.FrameSessions {
			var snap wire.SessionsResp
			_ = jsonUnmarshal(payload, &snap)
			for _, s := range snap.Sessions {
				if s.WorktreePath != "" {
					sessionID = s.ID
				}
			}
		}
	}
	// Bootstrap session may not have UseWorktree=true. Issue a
	// CREATE_SESSION for one that does.
	if err := wire.WriteJSON(conn, wire.FrameCreateSession, wire.CreateSpec{
		Name:        "wt",
		Shell:       "/bin/bash",
		Cols:        80,
		Rows:        24,
		UseWorktree: true,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && sessionID == "" {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			break
		}
		if ft == wire.FrameSessionEvent {
			var ev wire.SessionEvent
			_ = jsonUnmarshal(payload, &ev)
			if ev.Kind == wire.SessionEventAdded && ev.Session.WorktreePath != "" {
				sessionID = ev.Session.ID
			}
		}
	}
	if sessionID == "" {
		t.Fatalf("did not observe a worktree-backed session")
	}

	// Reach into the registry to dirty the worktree (avoids racing on
	// the daemon's session creation completing).
	entry := d.Registry().Get(sessionID)
	if entry == nil || entry.WorktreePath == "" {
		t.Fatalf("entry missing or worktreeless: %+v", entry)
	}
	if err := os.WriteFile(filepath.Join(entry.WorktreePath, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("dirty file: %v", err)
	}

	// 1. Kill without force → expect FrameError code worktree_dirty.
	if err := wire.WriteJSON(conn, wire.FrameKillSession, wire.KillSessionReq{
		SessionID: sessionID,
	}); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ft != wire.FrameError {
		t.Fatalf("expected FrameError, got %s", ft)
	}
	var werr wire.Error
	_ = jsonUnmarshal(payload, &werr)
	if werr.Code != wire.ErrCodeWorktreeDirty {
		t.Errorf("error code: got %q, want %q", werr.Code, wire.ErrCodeWorktreeDirty)
	}
	if werr.SessionID != sessionID {
		t.Errorf("error session_id: got %q, want %q", werr.SessionID, sessionID)
	}
	// Session must still be alive after a refused kill.
	if d.Registry().Get(sessionID) == nil {
		t.Errorf("session vanished after refused kill")
	}

	// 2. Kill with force → succeeds; expect SESSION_EVENT(removed).
	if err := wire.WriteJSON(conn, wire.FrameKillSession, wire.KillSessionReq{
		SessionID: sessionID,
		Force:     true,
	}); err != nil {
		t.Fatalf("force KillSession: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	gotRemoved := false
	for time.Now().Before(deadline) && !gotRemoved {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			break
		}
		if ft == wire.FrameSessionEvent {
			var ev wire.SessionEvent
			_ = jsonUnmarshal(payload, &ev)
			if ev.Kind == wire.SessionEventRemoved && ev.Session.ID == sessionID {
				gotRemoved = true
			}
		}
	}
	if !gotRemoved {
		t.Errorf("did not see SESSION_EVENT(removed) after force kill")
	}
}

// TestWelcomeAdvertisesBuildID verifies the daemon echoes its
// link-time buildinfo.BuildID in the Welcome frame, so a stale GUI
// can detect it and surface the version-mismatch banner. This is the
// Phase A handshake. Empty BuildID on the client side is tolerated;
// the daemon still answers with its own.
func TestWelcomeAdvertisesBuildID(t *testing.T) {
	skipOnWindows(t)
	// Override the package-level BuildID for the duration of this test.
	prev := buildinfo.BuildID
	buildinfo.BuildID = "test-build-xyz"
	t.Cleanup(func() { buildinfo.BuildID = prev })

	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	// Hello with no BuildID (simulating an older client) — daemon
	// should still respond with its own.
	w := handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if w.BuildID != "test-build-xyz" {
		t.Errorf("welcome BuildID: got %q, want %q", w.BuildID, "test-build-xyz")
	}
}

func TestRefusesProtocolMismatch(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()

	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{Version: 999, Client: "bad", Mode: wire.ModeControl}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	ft, _, err := wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ft != wire.FrameError {
		t.Errorf("expected ERROR, got %s", ft)
	}
}

func TestAttachUnknownSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	conn := dial(t, d)
	defer conn.Close()
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "test/0",
		Mode:    wire.ModeAttach,
		SessionID: "deadbeef-not-a-real-id",
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	ft, _, err := wire.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ft != wire.FrameError {
		t.Errorf("expected ERROR for missing session, got %s", ft)
	}
}
