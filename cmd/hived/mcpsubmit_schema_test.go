// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/acp"
)

const testResultSchema = `{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"severity":{"type":"string","enum":["high","low"]}},"required":["severity"],"additionalProperties":false}}},"required":["findings"],"additionalProperties":false}`

func schemaRoundTrip(t *testing.T, schema string, submit func(json.RawMessage) error, lines ...string) []map[string]any {
	t.Helper()
	var s *acp.Schema
	if schema != "" {
		var err error
		if s, err = acp.ParseSchema(json.RawMessage(schema)); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	runMCPSubmitSchema(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, submit, s)
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

func TestMCPSubmit_ToolsListCarriesSchema(t *testing.T) {
	r := schemaRoundTrip(t, testResultSchema, noSubmit, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tool := r[0]["result"].(map[string]any)["tools"].([]any)[0].(map[string]any)
	in := tool["inputSchema"].(map[string]any)
	data := in["properties"].(map[string]any)["data"].(map[string]any)
	if data["type"] != "object" || data["properties"].(map[string]any)["findings"] == nil {
		t.Errorf("data schema = %v, want the session's result schema", data)
	}
	if req := in["required"].([]any); len(req) != 1 || req[0] != "status" {
		t.Errorf("required = %v, want only status (an error result carries no data)", req)
	}
	if strings.Contains(strings.ToLower(mustJSON(t, tool)), "nonce") {
		t.Error("the tool mentions the nonce")
	}
	// The shared package-level definition is not mutated.
	if _, plain := submitTool["inputSchema"].(map[string]any)["properties"].(map[string]any)["data"].(map[string]any)["findings"]; plain {
		t.Error("submitTool was mutated")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMCPSubmit_PrecheckRejectsBadData(t *testing.T) {
	var got []json.RawMessage
	submit := func(r json.RawMessage) error { got = append(got, r); return nil }
	call := func(id int, args string) string {
		return `{"jsonrpc":"2.0","id":` + string(rune('0'+id)) + `,"method":"tools/call","params":{"name":"submit_result","arguments":` + args + `}}`
	}
	r := schemaRoundTrip(t, testResultSchema, submit,
		call(1, `{"status":"ok","data":{"findings":[{"severity":"urgent"}]}}`),
		call(2, `{"status":"ok"}`),
		call(3, `{"status":"error","summary":"could not"}`),
		call(4, `{"status":"ok","data":{"findings":[{"severity":"high"}]}}`),
	)
	res := func(i int) map[string]any { return r[i]["result"].(map[string]any) }
	text := func(i int) string { return res(i)["content"].([]any)[0].(map[string]any)["text"].(string) }
	if res(0)["isError"] != true || !strings.Contains(text(0), `data.findings[0].severity: "urgent" is not one of high|low`) || !strings.Contains(text(0), "call submit_result again") {
		t.Errorf("bad enum = %v", res(0))
	}
	if res(1)["isError"] != true || !strings.Contains(text(1), "data: required when status is ok") {
		t.Errorf("missing data = %v", res(1))
	}
	if res(2)["isError"] != false || res(3)["isError"] != false {
		t.Errorf("error status and good data must be accepted: %v %v", res(2), res(3))
	}
	if len(got) != 2 {
		t.Errorf("submitted %d results, want 2 (the rejected ones never reach the daemon)", len(got))
	}
}

// An empty object schema is valid and must reach tools/list as one MCP
// clients accept: "required" an array, not null.
func TestMCPSubmit_EmptyObjectSchemaListsArrayRequired(t *testing.T) {
	r := schemaRoundTrip(t, `{"type":"object","properties":{},"required":[],"additionalProperties":false}`, noSubmit,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tool := r[0]["result"].(map[string]any)["tools"].([]any)[0]
	if s := mustJSON(t, tool); strings.Contains(s, `"required":null`) {
		t.Errorf("tool = %s", s)
	}
}

func TestMCPSubmit_NoSchemaUnchanged(t *testing.T) {
	r := schemaRoundTrip(t, "", noSubmit, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tool := r[0]["result"].(map[string]any)["tools"].([]any)[0].(map[string]any)
	if mustJSON(t, tool) != mustJSON(t, submitTool) {
		t.Errorf("tool changed without a schema: %v", tool)
	}
}
