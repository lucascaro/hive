package main

import (
	"encoding/base64"
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
	allowed := []string{"127.0.0.1:0", "127.0.0.1:9222", "[::1]:0"}
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
		// Names are refused outright rather than resolved: net.Listen
		// re-resolves the name itself, so validating a resolution we
		// then discard would be check-then-use.
		"localhost:0",
		"localhost:9222",
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

// dialTestWS returns a live WebSocket conn attached to a server that
// discards everything. Session methods write RPC replies to s.ws, so a
// session built by hand needs a real conn or the first respond() panics.
func dialTestWS(t *testing.T) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				_ = c.Close()
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func stdinReq(id int, sid, payload string) rpcReq {
	b64 := base64.StdEncoding.EncodeToString([]byte(payload))
	return rpcReq{
		ID:     id,
		Method: "WriteStdin",
		Params: json.RawMessage(fmt.Sprintf(`{"id":%q,"b64":%q}`, sid, b64)),
	}
}

// TestWriteStdinPreservesArrivalOrder is the regression guard for spec 245.
//
// Keystrokes are a stream: their order IS the payload. The bridge used to
// run every RPC frame on its own goroutine, and a mutex around the write
// gives mutual exclusion, not ordering — so under load adjacent keys swapped
// and the shell ran a command nobody typed. net.Pipe is unbuffered, so every
// write here blocks until the reader takes it, which is exactly the
// backpressure that made the old code reorder.
func TestWriteStdinPreservesArrivalOrder(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	s := &session{
		ws:       dialTestWS(t),
		attaches: map[string]*wire.Client{"sid": wire.NewClient(client)},
	}
	defer s.shutdown()

	const frames = 200
	got := make(chan string, frames)
	go func() {
		for range frames {
			ft, payload, err := wire.ReadFrame(server)
			if err != nil || ft != wire.FrameData {
				close(got)
				return
			}
			got <- string(payload)
		}
	}()

	// Deliberately s.route, not s.writeStdinOrdered: the defect was the
	// ROUTING choice, so a test that calls the lane directly stays green
	// even when WriteStdin is sent back to `go dispatch`.
	for i := range frames {
		s.route(stdinReq(i, "sid", fmt.Sprintf("k%04d", i)))
	}

	for i := range frames {
		want := fmt.Sprintf("k%04d", i)
		select {
		case p, ok := <-got:
			if !ok {
				t.Fatalf("reader stopped early at frame %d", i)
			}
			if p != want {
				t.Fatalf("frame %d = %q, want %q — stdin arrived out of order", i, p, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for frame %d (%q)", i, want)
		}
	}
}

// TestShutdownTerminatesWhileStdinWriteIsBlocked pins the teardown ORDER.
//
// The stdin writer parks inside attachWriteFrame whenever the pty is not
// draining — normal in this suite, where a spec floods 60k lines. Only
// closeAll (which closes the attach client) aborts that write, so joining
// the writer first hangs forever and leaks the connection goroutine. Defers
// are LIFO, which makes the intuitive spelling the broken one; shutdown()
// exists so the order is stated once and tested here.
func TestShutdownTerminatesWhileStdinWriteIsBlocked(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	s := &session{
		ws:       dialTestWS(t),
		attaches: map[string]*wire.Client{"sid": wire.NewClient(client)},
	}

	// Nothing ever reads `server`, so the first frame blocks the writer.
	s.writeStdinOrdered(stdinReq(1, "sid", "wedged"))

	done := make(chan struct{})
	go func() {
		s.shutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not return with the stdin writer blocked — teardown deadlock")
	}
}
