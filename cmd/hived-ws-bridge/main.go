// hived-ws-bridge is the Layer B test harness shim: a localhost-only
// WebSocket bridge that translates JSON-RPC method calls from the
// browser-side Playwright runner into native wire-protocol frames
// against a real hived daemon, and pushes daemon events back to the
// browser as JSON-RPC notifications.
//
// It exists to make the Wails-fronted GUI testable end-to-end against
// the real daemon without spawning the native Wails process — the
// Vite-dev frontend imports a thin JS bridge (test/e2e-real/
// wails-bridge.ts) that talks to this shim over WS, in place of the
// generated Wails runtime + App bindings.
//
// Isolation contract: this binary refuses to start unless HIVE_SOCKET
// AND HIVE_STATE_DIR both point under /tmp / /private/tmp / /var/folders.
// It is exclusively a test tool — production code paths never reach it.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lucascaro/hive/internal/wire"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "WebSocket listen addr (0 picks a free port)")
	flag.Parse()

	if err := requireIsolation(); err != nil {
		log.Fatalf("hived-ws-bridge: %v", err)
	}
	if err := requireLoopback(*addr); err != nil {
		log.Fatalf("hived-ws-bridge: %v", err)
	}
	sockPath := os.Getenv("HIVE_SOCKET")

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	// Emit the bound URL to stdout so the test harness can read it
	// before any client connects.
	fmt.Printf("ws://%s/\n", ln.Addr().String())

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveWS(w, r, sockPath)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func requireIsolation() error {
	sock := os.Getenv("HIVE_SOCKET")
	state := os.Getenv("HIVE_STATE_DIR")
	if sock == "" || state == "" {
		return errors.New("HIVE_SOCKET and HIVE_STATE_DIR must be set")
	}
	tmpPrefixes := []string{os.TempDir(), "/tmp", "/private/tmp", "/var/folders"}
	for _, p := range []string{sock, state} {
		ok := false
		for _, pre := range tmpPrefixes {
			if strings.HasPrefix(p, pre+string(os.PathSeparator)) || p == pre {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("path %q is outside temp prefix", p)
		}
	}
	return nil
}

// requireLoopback rejects any -addr that is not bound to the loopback
// interface. The upgrader below accepts every Origin, which is only
// defensible while nothing off this machine can reach the listener —
// this is what makes "localhost-only listener" a guarantee rather than
// a comment. An empty host ("":0, ":9222") binds all interfaces, so it
// is refused too.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("addr %q binds every interface; use 127.0.0.1 or [::1]", addr)
	}
	// An IP literal only. Names are refused rather than resolved: this
	// guard's answer has to be the one net.Listen acts on, and Listen
	// re-resolves the name itself. Validating a resolution we then throw
	// away is check-then-use — the name could resolve to loopback here
	// and to something else at bind. Nothing needs a name anyway; the
	// harness passes 127.0.0.1:0.
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("addr %q: host must be an IP literal, not a name", addr)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("addr %q is not a loopback address", addr)
	}
	return nil
}

// --- WS session ---

// The listener being loopback-only is NOT origin protection: a
// WebSocket handshake is not subject to the same-origin policy, so any
// page open in a browser on this machine could otherwise connect to
// ws://127.0.0.1:<port>/ and drive the whole RPC surface — including
// WriteStdin into a live PTY and RemoveWorktree. So check the Origin
// too: no Origin at all (a non-browser client, which is how the Go
// tests connect) or a loopback origin on any port, which is what the
// harness's arbitrary Vite dev port needs.
var upgrader = websocket.Upgrader{CheckOrigin: originIsLocal}

func originIsLocal(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type session struct {
	ws       *websocket.Conn
	writeMu  sync.Mutex
	sockPath string

	// Ordered stdin lane. Keystrokes are a stream, so their order is part
	// of the payload: a goroutine per frame only takes the write mutex in
	// whatever order the scheduler picks, and under CPU load adjacent keys
	// swap (a test typing `HIVE_READY_mthhi3gn_1` had the shell echo back
	// `HIVE_READY_mthhig3n1_`, which then failed as "the command never
	// ran"). Mutual exclusion is not ordering.
	//
	// One goroutine drains this channel, so WriteStdin frames are applied
	// in arrival order. It is a lane of its own rather than inline work on
	// the read loop because attachWriteFrame backpressures when the pty
	// input buffer fills — normal here, where a spec floods 60k lines
	// without reading stdin — and that would stall ResizeSession /
	// CloseAttach / KillSession for the whole connection.
	stdin     chan rpcReq
	stdinOnce sync.Once
	stdinDone chan struct{}

	mu       sync.Mutex
	control  *wire.Client
	attaches map[string]*wire.Client // session id → attach conn
}

// writeStdinOrdered hands a WriteStdin frame to this connection's single
// stdin writer, starting it on first use. Buffered so an ordinary keystroke
// burst never blocks the read loop; a full buffer blocks, which is the
// backpressure we want (dropping input would corrupt the stream far worse
// than delaying it).
func (s *session) writeStdinOrdered(req rpcReq) {
	s.stdinOnce.Do(func() {
		s.stdin = make(chan rpcReq, 1024)
		s.stdinDone = make(chan struct{})
		go func() {
			defer close(s.stdinDone)
			for r := range s.stdin {
				s.dispatch(r)
			}
		}()
	})
	s.stdin <- req
}

// closeStdin drains and stops the stdin writer. Safe to call when the lane
// was never started.
// shutdown tears the connection down in the only order that terminates:
// closeAll first, so an in-flight attach write fails fast, then closeStdin
// to join the writer goroutine. serveWS defers this rather than the two
// calls separately — defers are LIFO, so writing them in the intuitive
// order produces exactly the deadlock closeStdin warns about.
func (s *session) shutdown() {
	s.closeAll()
	s.closeStdin()
}

// closeStdin drains and stops the stdin writer. Safe to call when the lane
// was never started.
//
// MUST run after closeAll, never before: the writer can be parked inside
// attachWriteFrame on pty backpressure, which is a normal state here, and
// only closing the attach client aborts that write. Joining the writer
// first hangs teardown forever and leaks the connection goroutine.
// TestShutdownTerminatesWhileStdinWriteIsBlocked pins this.
func (s *session) closeStdin() {
	if s.stdin == nil {
		return
	}
	close(s.stdin)
	select {
	case <-s.stdinDone:
	case <-time.After(5 * time.Second):
		// ponytail: closeAll should always unblock the writer, so this arm
		// is insurance — it turns a hung teardown (which wedges a whole CI
		// job) into a leaked goroutine in a process that is exiting anyway.
		log.Printf("ws-bridge: stdin writer did not drain within 5s")
	}
}

func serveWS(w http.ResponseWriter, r *http.Request, sockPath string) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}
	defer c.Close()
	s := &session{ws: c, sockPath: sockPath, attaches: make(map[string]*wire.Client)}
	defer s.shutdown()
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}
		var req rpcReq
		if err := json.Unmarshal(raw, &req); err != nil {
			s.respond(0, nil, fmt.Errorf("parse: %w", err))
			continue
		}
		// WriteStdin goes down the ordered stdin lane (see session.stdin);
		// it must not be dispatched concurrently with its own neighbours.
		if req.Method == "WriteStdin" {
			s.writeStdinOrdered(req)
			continue
		}
		// Everything else stays goroutine-per-request, unbounded by design:
		// the only client is the localhost Playwright runner, whose request
		// rate is bounded by the test code itself. A semaphore here could
		// deadlock the harness (a blocked ConnectControl holding a slot
		// while the test pumps WriteStdin). dispatch recovers panics.
		go s.dispatch(req)
	}
}

type rpcReq struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcResp struct {
	ID     int    `json:"id,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Event  string `json:"event,omitempty"`
	Args   []any  `json:"args,omitempty"`
}

func (s *session) respond(id int, result any, err error) {
	resp := rpcResp{ID: id, Result: result}
	if err != nil {
		resp.Error = err.Error()
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.ws.WriteJSON(resp)
}

func (s *session) emit(name string, args ...any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.ws.WriteJSON(rpcResp{Event: name, Args: args})
}

// parseParams decodes a request's params into v. Absent params — and
// literal null, JSON-RPC's spelling of absent — are allowed and leave
// v at its zero value (the JS bridge always sends at least {});
// malformed params are an error so a handler never runs on a silently
// zeroed struct.
func parseParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (s *session) dispatch(req rpcReq) {
	defer func() {
		if r := recover(); r != nil {
			s.respond(req.ID, nil, fmt.Errorf("panic: %v", r))
		}
	}()
	switch req.Method {
	case "ConnectControl":
		s.respond(req.ID, "", s.connectControl())
	case "CreateSession":
		var p wire.CreateSpec
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameCreateSession, p))
	case "KillSession":
		var p wire.KillSessionReq
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameKillSession, p))
	case "ListWorktrees":
		var p wire.ListWorktreesReq
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameListWorktrees, p))
	case "RemoveWorktree":
		var p wire.RemoveWorktreeReq
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameRemoveWorktree, p))
	case "CreateWorktree":
		var p wire.CreateWorktreeReq
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameCreateWorktree, p))
	case "DeleteBranch":
		var p wire.DeleteBranchReq
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameDeleteBranch, p))
	case "RenameWorktree":
		var p wire.RenameWorktreeReq
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameRenameWorktree, p))
	case "OpenSession":
		var p struct {
			ID   string `json:"id"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		info, err := s.openSession(p.ID, p.Cols, p.Rows)
		s.respond(req.ID, info, err)
	case "WriteStdin":
		var p struct {
			ID  string `json:"id"`
			B64 string `json:"b64"`
		}
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.writeStdin(p.ID, p.B64))
	case "ResizeSession":
		var p struct {
			ID   string `json:"id"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.attachWriteJSON(p.ID, wire.FrameResize, wire.Resize{Cols: p.Cols, Rows: p.Rows}))
	case "RequestScrollbackReplay":
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.attachWriteFrame(p.ID, wire.FrameRequestReplay, nil))
	case "CloseAttach":
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(req.Params, &p); err != nil {
			s.respond(req.ID, nil, err)
			return
		}
		s.respond(req.ID, "", s.closeAttach(p.ID))
	default:
		// Frontend imports a lot of methods we don't implement (Notify,
		// PickDirectory, etc.). Return empty success so boot doesn't trip.
		s.respond(req.ID, "", nil)
	}
}

// --- daemon plumbing ---

func (s *session) connectControl() error {
	s.mu.Lock()
	if s.control != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	cli, err := dialHandshake(s.sockPath, wire.Hello{Mode: wire.ModeControl, Client: "ws-bridge/control"})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.control = cli
	s.mu.Unlock()
	go s.controlReadLoop(cli)
	return nil
}

func (s *session) controlReadLoop(cli *wire.Client) {
	defer func() {
		s.mu.Lock()
		if s.control == cli {
			s.control = nil
		}
		s.mu.Unlock()
		_ = cli.Close()
		s.emit("control:disconnect", "")
	}()
	for {
		ft, payload, err := cli.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("ws-bridge: control read: %v", err)
			}
			return
		}
		if name, ok := wire.ControlEventName(ft); ok {
			s.emit(name, string(payload))
		}
	}
}

type attachInfo struct {
	SessionID string `json:"sessionId"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

func (s *session) openSession(id string, cols, rows int) (*attachInfo, error) {
	s.mu.Lock()
	if _, ok := s.attaches[id]; ok {
		s.mu.Unlock()
		return &attachInfo{SessionID: id, Cols: cols, Rows: rows}, nil
	}
	s.mu.Unlock()

	cli, err := dialHandshake(s.sockPath, wire.Hello{
		Mode: wire.ModeAttach, SessionID: id, Client: "ws-bridge/attach",
	})
	if err != nil {
		return nil, err
	}
	welcome := cli.Welcome()
	s.mu.Lock()
	s.attaches[id] = cli
	s.mu.Unlock()
	go s.attachReadLoop(id, cli)
	// Issue preferred size if non-zero and differs from welcome. Routed
	// through the locked writer — it races with WriteStdin dispatches.
	if cols > 0 && rows > 0 && (cols != welcome.Cols || rows != welcome.Rows) {
		_ = cli.WriteJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}
	return &attachInfo{SessionID: id, Cols: welcome.Cols, Rows: welcome.Rows}, nil
}

func (s *session) attachReadLoop(id string, cli *wire.Client) {
	defer func() {
		s.mu.Lock()
		if s.attaches[id] == cli {
			delete(s.attaches, id)
		}
		s.mu.Unlock()
		_ = cli.Close()
		s.emit("pty:disconnect", id)
	}()
	for {
		ft, payload, err := cli.ReadFrame()
		if err != nil {
			return
		}
		name, ok := wire.AttachEventName(ft)
		if !ok {
			continue
		}
		if ft == wire.FrameData {
			s.emit(name, id, base64.StdEncoding.EncodeToString(payload))
		} else {
			s.emit(name, id, string(payload))
		}
	}
}

func (s *session) writeStdin(id, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	return s.attachWriteFrame(id, wire.FrameData, data)
}

func (s *session) closeAttach(id string) error {
	s.mu.Lock()
	c, ok := s.attaches[id]
	if ok {
		delete(s.attaches, id)
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return c.Close()
}

func (s *session) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.control != nil {
		_ = s.control.Close()
		s.control = nil
	}
	for id, c := range s.attaches {
		_ = c.Close()
		delete(s.attaches, id)
	}
}

func (s *session) controlWriteJSON(t wire.FrameType, v any) error {
	s.mu.Lock()
	c := s.control
	s.mu.Unlock()
	if c == nil {
		return errors.New("no control connection")
	}
	return c.WriteJSON(t, v)
}

func (s *session) attachWriteFrame(id string, t wire.FrameType, p []byte) error {
	s.mu.Lock()
	c := s.attaches[id]
	s.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no attach for %s", id)
	}
	return c.WriteFrame(t, p)
}

func (s *session) attachWriteJSON(id string, t wire.FrameType, v any) error {
	s.mu.Lock()
	c := s.attaches[id]
	s.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no attach for %s", id)
	}
	return c.WriteJSON(t, v)
}

// --- helpers ---

func dialHandshake(sockPath string, hello wire.Hello) (*wire.Client, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(context.Background(), "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", sockPath, err)
	}
	return wire.Handshake(conn, hello)
}
