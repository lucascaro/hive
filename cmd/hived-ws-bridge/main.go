// hived-ws-bridge is the Layer B test harness shim: a localhost-only
// WebSocket bridge that translates JSON-RPC method calls from the
// browser-side Playwright runner into native wire-protocol frames
// against a real hived daemon, and pushes daemon events back to the
// browser as JSON-RPC notifications.
//
// It exists to make the Wails-fronted GUI testable end-to-end against
// the real daemon without spawning the native Wails process — the
// Vite-dev frontend imports a thin JS bridge (test/e2e-real/
// wails-bridge.js) that talks to this shim over WS, in place of the
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

// --- WS session ---

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true }, // localhost-only listener
}

type session struct {
	ws       *websocket.Conn
	writeMu  sync.Mutex
	sockPath string

	mu       sync.Mutex
	control  net.Conn
	attaches map[string]net.Conn // session id → attach conn
}

func serveWS(w http.ResponseWriter, r *http.Request, sockPath string) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}
	defer c.Close()
	s := &session{ws: c, sockPath: sockPath, attaches: make(map[string]net.Conn)}
	defer s.closeAll()
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
		go s.dispatch(req)
	}
}

type rpcReq struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcResp struct {
	ID     int         `json:"id,omitempty"`
	Result any `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
	Event  string      `json:"event,omitempty"`
	Args   []any       `json:"args,omitempty"`
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
		_ = json.Unmarshal(req.Params, &p)
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameCreateSession, p))
	case "KillSession":
		var p wire.KillSessionReq
		_ = json.Unmarshal(req.Params, &p)
		s.respond(req.ID, "", s.controlWriteJSON(wire.FrameKillSession, p))
	case "OpenSession":
		var p struct {
			ID   string `json:"id"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		_ = json.Unmarshal(req.Params, &p)
		info, err := s.openSession(p.ID, p.Cols, p.Rows)
		s.respond(req.ID, info, err)
	case "WriteStdin":
		var p struct {
			ID  string `json:"id"`
			B64 string `json:"b64"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.respond(req.ID, "", s.writeStdin(p.ID, p.B64))
	case "ResizeSession":
		var p struct {
			ID   string `json:"id"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.respond(req.ID, "", s.attachWriteJSON(p.ID, wire.FrameResize, wire.Resize{Cols: p.Cols, Rows: p.Rows}))
	case "RequestScrollbackReplay":
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.respond(req.ID, "", s.attachWriteFrame(p.ID, wire.FrameRequestReplay, nil))
	case "CloseAttach":
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &p)
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

	conn, welcome, err := dialHandshake(s.sockPath, wire.Hello{Mode: wire.ModeControl, Client: "ws-bridge/control"})
	if err != nil {
		return err
	}
	_ = welcome // build-id is irrelevant for tests
	s.mu.Lock()
	s.control = conn
	s.mu.Unlock()
	go s.controlReadLoop(conn)
	return nil
}

func (s *session) controlReadLoop(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		if s.control == conn {
			s.control = nil
		}
		s.mu.Unlock()
		_ = conn.Close()
		s.emit("control:disconnect", "")
	}()
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("ws-bridge: control read: %v", err)
			}
			return
		}
		switch ft {
		case wire.FrameSessions:
			s.emit("session:list", string(payload))
		case wire.FrameSessionEvent:
			s.emit("session:event", string(payload))
		case wire.FrameProjects:
			s.emit("project:list", string(payload))
		case wire.FrameProjectEvent:
			s.emit("project:event", string(payload))
		case wire.FrameError:
			s.emit("control:error", string(payload))
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

	conn, welcome, err := dialHandshake(s.sockPath, wire.Hello{
		Mode: wire.ModeAttach, SessionID: id, Client: "ws-bridge/attach",
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.attaches[id] = conn
	s.mu.Unlock()
	go s.attachReadLoop(id, conn)
	// Issue preferred size if non-zero and differs from welcome.
	if cols > 0 && rows > 0 && (cols != welcome.Cols || rows != welcome.Rows) {
		_ = wire.WriteJSON(conn, wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}
	return &attachInfo{SessionID: id, Cols: welcome.Cols, Rows: welcome.Rows}, nil
}

func (s *session) attachReadLoop(id string, conn net.Conn) {
	defer func() {
		s.mu.Lock()
		if s.attaches[id] == conn {
			delete(s.attaches, id)
		}
		s.mu.Unlock()
		_ = conn.Close()
		s.emit("pty:disconnect", id)
	}()
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return
		}
		switch ft {
		case wire.FrameData:
			s.emit("pty:data", id, base64.StdEncoding.EncodeToString(payload))
		case wire.FrameEvent:
			s.emit("pty:event", id, string(payload))
		case wire.FrameError:
			s.emit("pty:error", id, string(payload))
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
	return wire.WriteJSON(c, t, v)
}

func (s *session) attachWriteFrame(id string, t wire.FrameType, p []byte) error {
	s.mu.Lock()
	c := s.attaches[id]
	s.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no attach for %s", id)
	}
	return wire.WriteFrame(c, t, p)
}

func (s *session) attachWriteJSON(id string, t wire.FrameType, v any) error {
	s.mu.Lock()
	c := s.attaches[id]
	s.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no attach for %s", id)
	}
	return wire.WriteJSON(c, t, v)
}

// --- helpers ---

func dialHandshake(sockPath string, hello wire.Hello) (net.Conn, wire.Welcome, error) {
	hello.Version = wire.PROTOCOL_VERSION
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(context.Background(), "unix", sockPath)
	if err != nil {
		return nil, wire.Welcome{}, fmt.Errorf("dial %s: %w", sockPath, err)
	}
	if err := wire.WriteJSON(conn, wire.FrameHello, hello); err != nil {
		_ = conn.Close()
		return nil, wire.Welcome{}, fmt.Errorf("hello: %w", err)
	}
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, wire.Welcome{}, fmt.Errorf("welcome read: %w", err)
	}
	if ft == wire.FrameError {
		_ = conn.Close()
		var werr wire.Error
		_ = json.Unmarshal(payload, &werr)
		return nil, wire.Welcome{}, fmt.Errorf("handshake refused: %s", werr.Message)
	}
	if ft != wire.FrameWelcome {
		_ = conn.Close()
		return nil, wire.Welcome{}, fmt.Errorf("unexpected frame %s", ft)
	}
	var w wire.Welcome
	_ = json.Unmarshal(payload, &w)
	return conn, w, nil
}
