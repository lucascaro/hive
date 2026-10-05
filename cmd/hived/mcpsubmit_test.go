package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// mcpRoundTrip feeds lines to runMCPSubmit and returns its replies.
func mcpRoundTrip(t *testing.T, submit func(json.RawMessage) error, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	runMCPSubmit(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, submit)
	var replies []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("reply %q: %v", l, err)
		}
		replies = append(replies, m)
	}
	return replies
}

func noSubmit(json.RawMessage) error { return errors.New("unexpected submit") }

func TestToolsListAdvertisesSubmitResult(t *testing.T) {
	r := mcpRoundTrip(t, noSubmit,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(r) != 2 {
		t.Fatalf("replies = %v, want 2 (the notification gets none)", r)
	}
	init := r[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["capabilities"].(map[string]any)["tools"] == nil {
		t.Errorf("initialize = %v, want the client's version and a tools capability", init)
	}
	tools := r[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "submit_result" {
		t.Errorf("tools = %v, want exactly submit_result", tools)
	}
}

// The model sees the schema; the nonce must never be in it.
func TestNonceNotInToolSchema(t *testing.T) {
	b, _ := json.Marshal(submitTool)
	if strings.Contains(strings.ToLower(string(b)), "nonce") {
		t.Errorf("tool schema mentions the nonce: %s", b)
	}
}

func TestToolsCallValidatesStatus(t *testing.T) {
	var got json.RawMessage
	submit := func(r json.RawMessage) error { got = r; return nil }
	r := mcpRoundTrip(t, submit,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"submit_result","arguments":{"status":"done"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"other","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"submit_result","arguments":{"status":"ok","summary":"s"}}}`,
	)
	if res := r[0]["result"].(map[string]any); res["isError"] != true {
		t.Errorf("bad status = %v, want isError", res)
	}
	if r[1]["error"] == nil {
		t.Errorf("unknown tool = %v, want a JSON-RPC error", r[1])
	}
	if res := r[2]["result"].(map[string]any); res["isError"] != false || string(got) != `{"status":"ok","summary":"s"}` {
		t.Errorf("good call = %v, submitted %s; want success and the arguments as the result", res, got)
	}
}

// fakeDaemon accepts one session connection, answers the handshake,
// sends an unrelated snapshot first, then answers the SUBMIT_RESULT with
// reply. It reports the HELLO and the request it read.
func fakeDaemon(t *testing.T, reply func(net.Conn)) (sock string, hello chan wire.Hello, req chan wire.SubmitResultReq) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix socket fake")
	}
	dir, err := os.MkdirTemp("", "mcps")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock = filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	hello, req = make(chan wire.Hello, 1), make(chan wire.SubmitResultReq, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, p, _ := wire.ReadFrame(conn)
		var h wire.Hello
		_ = json.Unmarshal(p, &h)
		hello <- h
		b, _ := json.Marshal(wire.Welcome{Version: wire.PROTOCOL_VERSION})
		_ = wire.WriteFrame(conn, wire.FrameWelcome, b)
		_ = wire.WriteFrame(conn, wire.FrameSessions, []byte(`{"sessions":[]}`))
		ft, p, _ := wire.ReadFrame(conn)
		if ft != wire.FrameSubmitResult {
			return
		}
		var r wire.SubmitResultReq
		_ = json.Unmarshal(p, &r)
		req <- r
		reply(conn)
	}()
	return sock, hello, req
}

func TestToolsCallSendsSubmitFrame(t *testing.T) {
	sock, hello, req := fakeDaemon(t, func(c net.Conn) { _ = wire.WriteFrame(c, wire.FrameSubmitResultOK, []byte(`{}`)) })
	env := map[string]string{"HIVE_SESSION_ID": "s1", "HIVE_SOCKET": sock, "HIVE_SUBMIT_NONCE": "n0nce"}
	r := mcpRoundTrip(t, submitOverSocket(func(k string) string { return env[k] }),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"submit_result","arguments":{"status":"ok"}}}`)
	if res := r[0]["result"].(map[string]any); res["isError"] != false {
		t.Fatalf("tools/call = %v, want success", res)
	}
	h := <-hello
	if h.Mode != wire.ModeSession || h.SessionID != "s1" {
		t.Errorf("HELLO = %+v, want a session connection for s1", h)
	}
	got := <-req
	if got.Nonce != "n0nce" || string(got.Result) != `{"status":"ok"}` {
		t.Errorf("SUBMIT_RESULT = %+v, want the env nonce and the arguments", got)
	}
}

// A refusal reaches the model as a tool error, so it is not told its
// result was recorded.
func TestToolsCallReportsRejection(t *testing.T) {
	sock, _, _ := fakeDaemon(t, func(c net.Conn) {
		b, _ := json.Marshal(wire.Error{Code: wire.ErrCodeSubmitRejected, Message: "no turn is running"})
		_ = wire.WriteFrame(c, wire.FrameError, b)
	})
	env := map[string]string{"HIVE_SESSION_ID": "s1", "HIVE_SOCKET": sock, "HIVE_SUBMIT_NONCE": "n"}
	r := mcpRoundTrip(t, submitOverSocket(func(k string) string { return env[k] }),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"submit_result","arguments":{"status":"ok"}}}`)
	res := r[0]["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if res["isError"] != true || !strings.Contains(text, "no turn is running") {
		t.Errorf("rejected submit = %v, want isError with the daemon's reason", res)
	}
}

func TestSubmitOutsideHiveFails(t *testing.T) {
	err := submitOverSocket(func(string) string { return "" })(json.RawMessage(`{"status":"ok"}`))
	if err == nil || !strings.Contains(err.Error(), "not started by Hive") {
		t.Errorf("submit with no env = %v, want a not-started-by-Hive error", err)
	}
}
