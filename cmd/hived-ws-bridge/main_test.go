package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lucascaro/hive/internal/wire"
)

// dialTestBridge spins up serveWS against a daemon socket that does not
// exist (dispatch-level behavior needs no live daemon) and returns a
// connected WS client. All paths live under t.TempDir(): the isolation
// guard runs only in main(), and nothing here can touch real state.
func dialTestBridge(t *testing.T) *websocket.Conn {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWS(w, r, sockPath)
	}))
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

// roundTrip sends one JSON-RPC request and reads frames until the
// response with the matching id arrives (skipping event notifications).
func roundTrip(t *testing.T, ws *websocket.Conn, id int, method, rawParams string) rpcResp {
	t.Helper()
	req := fmt.Sprintf(`{"id":%d,"method":%q,"params":%s}`, id, method, rawParams)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = ws.SetReadDeadline(deadline)
		_, raw, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("read response for %s: %v", method, err)
		}
		var resp rpcResp
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal response: %v (%s)", err, raw)
		}
		if resp.ID == id {
			return resp
		}
	}
}

func TestDispatchRejectsMalformedParams(t *testing.T) {
	ws := dialTestBridge(t)

	methods := []string{
		"CreateSession", "KillSession", "OpenSession", "WriteStdin",
		"ResizeSession", "RequestScrollbackReplay", "CloseAttach",
	}
	for i, m := range methods {
		t.Run(m+"/number", func(t *testing.T) {
			resp := roundTrip(t, ws, 100+i, m, `42`)
			if !strings.Contains(resp.Error, "invalid params") {
				t.Errorf("%s with numeric params: error = %q, want invalid params", m, resp.Error)
			}
		})
		t.Run(m+"/array", func(t *testing.T) {
			resp := roundTrip(t, ws, 200+i, m, `["x"]`)
			if !strings.Contains(resp.Error, "invalid params") {
				t.Errorf("%s with array params: error = %q, want invalid params", m, resp.Error)
			}
		})
	}
}

// TestDispatchEmptyParamsStillPermissive pins the happy-path contract:
// {} params must reach the handler, whose failure (no daemon behind
// the socket) is an execution error, not a parse error.
func TestDispatchEmptyParamsStillPermissive(t *testing.T) {
	ws := dialTestBridge(t)
	resp := roundTrip(t, ws, 1, "CreateSession", `{}`)
	if !strings.Contains(resp.Error, "no control connection") {
		t.Errorf("CreateSession with {}: error = %q, want execution error %q", resp.Error, "no control connection")
	}
	// Literal null is JSON-RPC's spelling of absent params — it must
	// reach the handler like {}, not be rejected as malformed.
	resp = roundTrip(t, ws, 2, "CreateSession", `null`)
	if !strings.Contains(resp.Error, "no control connection") {
		t.Errorf("CreateSession with null: error = %q, want execution error %q", resp.Error, "no control connection")
	}
	// Unknown methods keep returning empty success.
	resp = roundTrip(t, ws, 3, "Notify", `{}`)
	if resp.Error != "" {
		t.Errorf("unknown method: error = %q, want success", resp.Error)
	}
}

// TestConcurrentAttachWritesAreSerialized proves frame writes from
// concurrent goroutines cannot interleave mid-frame. Without the write
// mutex in wire.Client, wire.WriteFrame's two Writes (header, payload)
// from racing goroutines corrupted the stream; this test fails on that
// code.
func TestConcurrentAttachWritesAreSerialized(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	s := &session{attaches: map[string]*wire.Client{"sid": wire.NewClient(client)}}

	const goroutines, frames = 10, 50
	got := make(map[string]int)
	done := make(chan error, 1)
	go func() {
		for range goroutines * frames {
			ft, payload, err := wire.ReadFrame(server)
			if err != nil {
				done <- fmt.Errorf("read frame after %d ok frames: %w", len(got), err)
				return
			}
			if ft != wire.FrameData {
				done <- fmt.Errorf("frame type %v, want FrameData", ft)
				return
			}
			got[string(payload)]++
		}
		done <- nil
	}()

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range frames {
				payload := fmt.Sprintf("g%02d-i%02d", g, i)
				if err := s.attachWriteFrame("sid", wire.FrameData, []byte(payload)); err != nil {
					t.Errorf("attachWriteFrame %s: %v", payload, err)
					return
				}
			}
		})
	}
	wg.Wait()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reader did not finish; stream likely corrupted")
	}

	if len(got) != goroutines*frames {
		t.Fatalf("distinct payloads = %d, want %d", len(got), goroutines*frames)
	}
	for p, n := range got {
		if n != 1 {
			t.Errorf("payload %q seen %d times, want 1", p, n)
		}
	}
}

// TestRequireLoopbackRefusesOffHostBinds pins the assumption the
// permissive CheckOrigin rests on: this bridge speaks for a real hived
// daemon with no authentication of its own, so a non-loopback bind
// would hand every session on the machine to anything that can reach
// the port.
func TestRequireLoopbackRefusesOffHostBinds(t *testing.T) {
	allowed := []string{"127.0.0.1:0", "127.0.0.1:9222", "[::1]:0", "localhost:0"}
	for _, addr := range allowed {
		if err := requireLoopback(addr); err != nil {
			t.Errorf("requireLoopback(%q) = %v, want nil", addr, err)
		}
	}
	refused := []string{
		":0",              // every interface
		":9222",           // every interface
		"0.0.0.0:9222",    // every interface, explicitly
		"[::]:9222",       // every interface, v6
		"192.168.1.10:80", // a LAN address
		"example.com:80",  // a name that is not localhost
		"127.0.0.1",       // missing port — SplitHostPort fails
	}
	for _, addr := range refused {
		if err := requireLoopback(addr); err == nil {
			t.Errorf("requireLoopback(%q) = nil, want an error", addr)
		}
	}
}

// TestRequireIsolationRefusesRealState is the other half of the guard:
// the bridge must never be pointed at a developer's live hive state.
func TestRequireIsolationRefusesRealState(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name        string
		sock, state string
		wantErr     bool
	}{
		{"both in tmp", filepath.Join(tmp, "hived.sock"), tmp, false},
		{"unset", "", "", true},
		{"only socket set", filepath.Join(tmp, "hived.sock"), "", true},
		{"state outside tmp", filepath.Join(tmp, "hived.sock"), "/Users/someone/.hive", true},
		{"socket outside tmp", "/Users/someone/.hive/hived.sock", tmp, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HIVE_SOCKET", tc.sock)
			t.Setenv("HIVE_STATE_DIR", tc.state)
			err := requireIsolation()
			if tc.wantErr != (err != nil) {
				t.Fatalf("requireIsolation() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestOriginIsLocal: the loopback bind is not origin protection. A
// WebSocket handshake is not subject to the same-origin policy, so
// without this check any page open in a browser on this machine could
// connect and drive the whole RPC surface — WriteStdin into a live
// PTY included.
func TestOriginIsLocal(t *testing.T) {
	check := func(origin string) bool {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return originIsLocal(r)
	}
	// No Origin header at all: a non-browser client, which is how the
	// Go tests and any CLI connect. Browsers always send one.
	if !check("") {
		t.Error("a request with no Origin was rejected")
	}
	for _, o := range []string{
		"http://localhost:5175", // the harness's Vite dev server
		"http://127.0.0.1:5173",
		"http://[::1]:9222",
		"https://localhost",
	} {
		if !check(o) {
			t.Errorf("local origin %q was rejected", o)
		}
	}
	for _, o := range []string{
		"http://evil.example.com",
		"https://127.0.0.1.evil.example.com", // suffix trickery
		"http://192.168.1.10:5173",
		"http://localhost.evil.example.com",
		"::not a url::",
	} {
		if check(o) {
			t.Errorf("foreign origin %q was accepted", o)
		}
	}
}
