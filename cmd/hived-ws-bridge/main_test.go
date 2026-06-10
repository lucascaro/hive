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
	// Unknown methods keep returning empty success.
	resp = roundTrip(t, ws, 2, "Notify", `{}`)
	if resp.Error != "" {
		t.Errorf("unknown method: error = %q, want success", resp.Error)
	}
}

// TestConcurrentAttachWritesAreSerialized proves frame writes from
// concurrent goroutines cannot interleave mid-frame. Before lockedConn,
// wire.WriteFrame's two Writes (header, payload) from racing goroutines
// corrupted the stream; this test fails on that code.
func TestConcurrentAttachWritesAreSerialized(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	s := &session{attaches: map[string]*lockedConn{"sid": {c: client}}}

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
