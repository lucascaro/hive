// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func pinRoundTrip(t *testing.T, pin func(title, body string) error, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	runMCPPin(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, pin)
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

func toolResult(t *testing.T, reply map[string]any) (string, bool) {
	t.Helper()
	res, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", reply)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	return text, res["isError"].(bool)
}

func TestMCPPinListsTool(t *testing.T) {
	r := pinRoundTrip(t, nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := r[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "pin" {
		t.Fatalf("tools = %v, want exactly pin", tools)
	}
	// The session's credentials never appear in what the model sees.
	b, _ := json.Marshal(tools)
	for _, s := range []string{"HIVE_", "token", "session_id"} {
		if strings.Contains(string(b), s) {
			t.Errorf("tool definition mentions %q", s)
		}
	}
}

func TestMCPPinCallsAddPin(t *testing.T) {
	var gotTitle, gotBody string
	r := pinRoundTrip(t, func(title, body string) error {
		gotTitle, gotBody = title, body
		return nil
	}, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"pin","arguments":{"title":"Root cause","body":"It was **DNS**."}}}`)
	text, isErr := toolResult(t, r[0])
	if isErr || text != "Pinned." {
		t.Fatalf("result = %q isError=%v", text, isErr)
	}
	if gotTitle != "Root cause" || gotBody != "It was **DNS**." {
		t.Fatalf("pinned %q / %q", gotTitle, gotBody)
	}
}

func TestMCPPinSurfacesDaemonError(t *testing.T) {
	r := pinRoundTrip(t, func(string, string) error {
		return errors.New("pin_too_long: body is 70000 bytes")
	}, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pin","arguments":{"title":"t","body":"b"}}}`)
	text, isErr := toolResult(t, r[0])
	if !isErr || !strings.Contains(text, "pin_too_long") {
		t.Fatalf("result = %q isError=%v", text, isErr)
	}
}

func TestMCPPinRejectsEmptyArgsWithoutCalling(t *testing.T) {
	called := false
	r := pinRoundTrip(t, func(string, string) error { called = true; return nil },
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pin","arguments":{"title":" ","body":"b"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"submit_result","arguments":{}}}`,
	)
	if _, isErr := toolResult(t, r[0]); !isErr || called {
		t.Fatalf("empty title: isError=%v called=%v", isErr, called)
	}
	if r[1]["error"] == nil {
		t.Fatalf("an unknown tool must be a JSON-RPC error: %v", r[1])
	}
}

func TestPinOverSocketNeedsHiveEnv(t *testing.T) {
	err := pinOverSocket(func(string) string { return "" })("t", "b")
	if err == nil || !strings.Contains(err.Error(), "not started by Hive") {
		t.Fatalf("err = %v", err)
	}
}
