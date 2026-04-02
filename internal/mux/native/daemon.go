package muxnative

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SockPath returns the Unix socket path for the daemon.
func SockPath() string {
	dir := os.Getenv("HIVE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "hive")
	}
	return filepath.Join(dir, "mux.sock")
}

// RunDaemon starts the mux daemon: creates the Unix socket, starts the manager,
// and handles incoming connections. It blocks until the socket is closed or an
// unrecoverable error occurs. The caller is responsible for daemonising the process
// (Setsid, closing fds, etc.).
func RunDaemon(sockPath string, logPath string) error {
	// Redirect log output to a file if provided.
	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			log.SetOutput(f)
		}
	}

	// Remove stale socket file.
	os.Remove(sockPath)

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sockPath, err)
	}
	defer l.Close()
	defer os.Remove(sockPath)

	// Restrict socket access to the owner only (prevent other local users from connecting).
	if err := os.Chmod(sockPath, 0o600); err != nil {
		log.Printf("warning: failed to set socket permissions: %v", err)
	}

	log.Printf("hive mux-daemon: listening on %s", sockPath)

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn)
	}
}

// handleConn serves a single client connection: reads one request, dispatches
// it, and either writes one response and closes, or (for attach) keeps the
// connection open for raw bidirectional streaming.
func handleConn(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := readMsg(conn, &req); err != nil {
		return
	}

	mgr := defaultMgr

	switch req.Op {
	case "ping":
		logWriteErr(conn, writeMsg(conn, okResp()))

	case "create_session":
		err := mgr.createSession(req.Session, req.WindowName, req.WorkDir, req.Cmd)
		if err != nil {
			logWriteErr(conn, writeMsg(conn, errResp(err.Error())))
			return
		}
		logWriteErr(conn, writeMsg(conn, okResp()))

	case "session_exists":
		logWriteErr(conn, writeMsg(conn, Response{OK: true, Bool: mgr.sessionExists(req.Session)}))

	case "kill_session":
		if err := mgr.killSession(req.Session); err != nil {
			logWriteErr(conn, writeMsg(conn, errResp(err.Error())))
			return
		}
		logWriteErr(conn, writeMsg(conn, okResp()))

	case "list_session_names":
		logWriteErr(conn, writeMsg(conn, Response{OK: true, Strings: mgr.listSessionNames()}))

	case "create_window":
		idx, err := mgr.createWindow(req.Session, req.WindowName, req.WorkDir, req.Cmd)
		if err != nil {
			logWriteErr(conn, writeMsg(conn, errResp(err.Error())))
			return
		}
		logWriteErr(conn, writeMsg(conn, Response{OK: true, Int: idx}))

	case "window_exists":
		logWriteErr(conn, writeMsg(conn, Response{OK: true, Bool: mgr.windowExists(req.Target)}))

	case "kill_window":
		if err := mgr.killWindow(req.Target); err != nil {
			logWriteErr(conn, writeMsg(conn, errResp(err.Error())))
			return
		}
		logWriteErr(conn, writeMsg(conn, okResp()))

	case "rename_window":
		if err := mgr.renameWindow(req.Target, req.NewName); err != nil {
			logWriteErr(conn, writeMsg(conn, errResp(err.Error())))
			return
		}
		logWriteErr(conn, writeMsg(conn, okResp()))

	case "list_windows":
		entries, err := mgr.listWindows(req.Session)
		if err != nil {
			logWriteErr(conn, writeMsg(conn, errResp(err.Error())))
			return
		}
		wins := make([]WindowInfo, len(entries))
		for i, e := range entries {
			wins[i] = WindowInfo{Index: e.idx, Name: e.name}
		}
		logWriteErr(conn, writeMsg(conn, Response{OK: true, Windows: wins}))

	case "capture_pane", "capture_pane_raw":
		p := mgr.paneByTarget(req.Target)
		if p == nil || p.isDead() {
			logWriteErr(conn, writeMsg(conn, errResp("pane not found: "+req.Target)))
			return
		}
		content := p.capture(req.Lines)
		logWriteErr(conn, writeMsg(conn, Response{OK: true, Content: content}))

	case "attach":
		handleAttach(conn, req.Target)

	default:
		logWriteErr(conn, writeMsg(conn, errResp("unknown op: "+req.Op)))
	}
}

// logWriteErr logs a protocol write error. The connection will be closed by the
// deferred conn.Close() in handleConn, so this is informational only.
func logWriteErr(conn net.Conn, err error) {
	if err != nil {
		log.Printf("daemon: write to %s failed: %v", conn.RemoteAddr(), err)
	}
}

// handleAttach proxies stdin/stdout between the client and the PTY master.
// After sending OK, the connection becomes a raw bidirectional byte stream:
// - bytes from client  → PTY master (stdin for the agent process)
// - bytes from PTY master → client (stdout seen by the user)
// The client signals detach by closing its write side (half-close).
func handleAttach(conn net.Conn, target string) {
	p := defaultMgr.paneByTarget(target)
	if p == nil || p.isDead() {
		writeMsg(conn, errResp("pane not found: "+target)) //nolint:errcheck
		return
	}

	// Confirm attach to client; after this the connection is raw.
	if err := writeMsg(conn, okResp()); err != nil {
		return
	}

	// Fan-out: new PTY output also gets written to conn.
	p.setAttachWriter(conn)
	defer p.setAttachWriter(nil)

	// Forward client bytes to PTY master.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				p.ptm.Write(buf[:n]) //nolint:errcheck
			}
			if err != nil {
				return
			}
		}
	}()

	wg.Wait()
}

// EnsureRunning makes sure the daemon is alive. If not, it forks a new daemon
// process and waits up to 1 s for it to start accepting connections.
func EnsureRunning(sockPath string, logPath string) error {
	if Ping(sockPath) == nil {
		return nil // already running
	}

	if err := spawnDaemon(sockPath, logPath); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}

	// Poll until the socket is up (up to 2 s in 50 ms increments).
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		if Ping(sockPath) == nil {
			return nil
		}
	}
	return fmt.Errorf("mux daemon did not start within 2 s")
}

// Ping attempts a round-trip with the daemon and returns nil on success.
func Ping(sockPath string) error {
	conn, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(200 * time.Millisecond)) //nolint:errcheck
	if err := writeMsg(conn, Request{Op: "ping"}); err != nil {
		return err
	}
	var resp Response
	return readMsg(conn, &resp)
}

// spawnDaemon fork-execs the current binary with the "mux-daemon" sub-command,
// detached from the calling process's session.
func spawnDaemon(sockPath string, logPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	args := []string{"mux-daemon", "--sock", sockPath}
	if logPath != "" {
		args = append(args, "--log", logPath)
	}

	cmd := daemonCmd(exe, args...)
	return cmd.Start()
}

// io helpers reused by tests.
var _ io.Writer = (*net.UnixConn)(nil)
